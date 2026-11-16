"""Persistent, offline mod state engine. No device hooks or package scripts are executed."""
import copy
import hashlib
import json
import os
import re
import shutil
import stat
import tempfile
import threading
import zipfile
import fcntl
from contextlib import contextmanager
from pathlib import Path

IDENT = re.compile(r"^[a-z0-9][a-z0-9._-]{0,63}$")
DIGEST = re.compile(r"^[a-f0-9]{64}$")
_LOCK = threading.RLock()


class ModError(ValueError):
    pass


def _id(value):
    if not isinstance(value, str) or not IDENT.fullmatch(value) or value in (".", ".."):
        raise ModError("invalid identifier")
    return value


def _digest(value):
    if not isinstance(value, str) or not DIGEST.fullmatch(value.lower()):
        raise ModError("invalid digest")
    return value.lower()


def _atomic(path, data):
    path.parent.mkdir(parents=True, exist_ok=True)
    fd, name = tempfile.mkstemp(dir=str(path.parent), prefix=".pending-")
    try:
        with os.fdopen(fd, "w", encoding="utf-8") as stream:
            json.dump(data, stream, sort_keys=True, indent=2)
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(name, path)
        directory = os.open(str(path.parent), os.O_RDONLY)
        try:
            os.fsync(directory)
        finally:
            os.close(directory)
    finally:
        if os.path.exists(name):
            os.unlink(name)


def safe_extract(archive, destination, max_bytes=128 * 1024 * 1024):
    """Extract regular files only; reject links, duplicates and archive bombs."""
    destination = Path(destination)
    destination.mkdir(parents=True, exist_ok=True)
    total = 0
    seen = {}
    with zipfile.ZipFile(archive) as source:
        for member in source.infolist():
            name = member.filename
            parts = Path(name).parts
            mode = member.external_attr >> 16
            kind = stat.S_IFMT(mode)
            if (not name or name.startswith("/") or "\\" in name or
                    any(p in ("", ".", "..") for p in parts) or
                    ":" in parts[0] or name.rstrip("/") in seen or
                    kind not in (0, stat.S_IFREG, stat.S_IFDIR)):
                raise ModError("unsafe archive member")
            canonical = name.rstrip("/")
            # A file may not also be an ancestor of another entry and paths are
            # compared after removing the ZIP directory marker.
            if any((canonical.startswith(old + "/") and old_kind != stat.S_IFDIR) or
                   old.startswith(canonical + "/")
                   for old, old_kind in seen.items()):
                raise ModError("conflicting archive paths")
            seen[canonical] = stat.S_IFDIR if member.is_dir() else stat.S_IFREG
            total += member.file_size
            if total > max_bytes:
                raise ModError("archive exceeds size limit")
            target = destination.joinpath(*parts)
            if member.is_dir():
                target.mkdir(parents=True, exist_ok=True)
            else:
                target.parent.mkdir(parents=True, exist_ok=True)
                with source.open(member) as src, target.open("xb") as dst:
                    shutil.copyfileobj(src, dst)


class Registry:
    """State mutations use a durable pending journal and a generated boot manifest.

    The caller supplies a private, Modloader-owned root. A deployment must use
    /data/prime-unlimited; tests may use a temporary root.
    """
    def __init__(self, root):
        self.root = Path(root).resolve()
        self.registry = self.root / "registry" / "state.json"
        self.journal = self.root / "registry" / "pending.json"
        self.startup = self.root / "registry" / "startup.json"
        self.transactions = self.root / "registry" / "transactions"
        self.lockfile = self.root / "registry" / "mutation.lock"
        self.root.mkdir(parents=True, exist_ok=True)
        self.recover()

    def _empty(self):
        return {"schemaVersion": 1, "mods": {}, "libraries": {}}

    def read(self):
        if not self.registry.exists():
            return self._empty()
        state = json.loads(self.registry.read_text(encoding="utf-8"))
        if (not isinstance(state, dict) or state.get("schemaVersion") != 1 or
                not isinstance(state.get("mods"), dict) or
                not isinstance(state.get("libraries"), dict)):
            raise ModError("unknown registry format")
        for mid, mod in state["mods"].items():
            _id(mid)
            if not isinstance(mod, dict) or not isinstance(mod.get("version"), str):
                raise ModError("invalid mod registry entry")
        for key, lib in state["libraries"].items():
            if not isinstance(key, str) or not isinstance(lib, dict):
                raise ModError("invalid library registry entry")
        return state

    @contextmanager
    def mutation_lock(self, blocking=False):
        """Cross-process lock; the in-process lock only protects threads."""
        self.lockfile.parent.mkdir(parents=True, exist_ok=True)
        with _LOCK, self.lockfile.open("a+") as stream:
            try:
                fcntl.flock(stream, fcntl.LOCK_EX | (0 if blocking else fcntl.LOCK_NB))
            except BlockingIOError as exc:
                raise ModError("another mutation is in progress") from exc
            try:
                yield
            finally:
                fcntl.flock(stream, fcntl.LOCK_UN)

    def recover(self):
        with _LOCK:
            if self.journal.exists():
                pending = json.loads(self.journal.read_text(encoding="utf-8"))
                if pending.get("phase") == "committed" and "next" in pending:
                    _atomic(self.registry, pending["next"])
                elif "previous" in pending:
                    _atomic(self.registry, pending["previous"])
                elif "next" in pending:  # compatibility with baseline journals
                    _atomic(self.registry, pending["next"])
                self.journal.unlink()
            self._startup(self.read())

    def _startup(self, state):
        enabled = []
        visiting, visited = set(), set()

        def add(mid):
            if mid in visiting:
                raise ModError("cyclic installed dependency graph")
            if mid in visited:
                return any(item["id"] == mid for item in enabled)
            visiting.add(mid)
            mod = state["mods"][mid]
            dependencies_valid = True
            for dep in sorted(mod.get("dependencies", []), key=lambda x: x["id"]):
                dependency = state["mods"].get(dep["id"])
                if not dependency or not dependency.get("enabled") or not add(dep["id"]):
                    dependencies_valid = False
            visiting.remove(mid)
            visited.add(mid)
            if mod.get("enabled") and mod.get("entrypoint") and dependencies_valid:
                lib_paths = [state["libraries"][key]["path"] for key in mod.get("libraries", [])]
                enabled.append({"id": mid, "version": mod["version"],
                                "root": "mods/installed/%s/%s" % (mid, mod["version"]),
                                "entrypoint": mod["entrypoint"], "libraries": lib_paths})
                return True
            return False

        for mid in sorted(state["mods"]):
            add(mid)
        _atomic(self.startup, {"schemaVersion": 1, "enabled": enabled})

    def _commit(self, state):
        previous = self.read()
        _atomic(self.journal, {"phase": "prepared", "previous": previous, "next": state})
        _atomic(self.registry, state)
        _atomic(self.journal, {"phase": "committed", "previous": previous, "next": state})
        self._startup(state)
        self.journal.unlink()

    def libraries(self):
        state = self.read()
        return [{"key": key, **value} for key, value in sorted(state["libraries"].items())]

    def installed(self):
        return self.read()["mods"]

    def dependents(self, mid):
        mid = _id(mid)
        return sorted(name for name, mod in self.read()["mods"].items()
                      if any(d.get("id") == mid for d in mod.get("dependencies", [])))

    def transaction_status(self, transaction_id=None):
        if not self.transactions.exists():
            return [] if transaction_id is None else None
        records = []
        for path in sorted(self.transactions.glob("*.json")):
            data = json.loads(path.read_text(encoding="utf-8"))
            if transaction_id is None or data.get("id") == transaction_id:
                records.append(data)
        return records if transaction_id is None else (records[0] if records else None)

    @staticmethod
    def library_key(dep):
        return "|".join((_id(dep["id"]), _id(dep["version"]),
                         _id(dep["architecture"]), _id(dep["abi"]),
                         _digest(dep["sha256"])))

    def install(self, spec, package, model, firmware, architecture, abi):
        """Install a previously trusted catalog record from a local archive.

        Approval must be supplied by the trusted catalog, never package contents.
        """
        with _LOCK:
            mid = _id(spec["id"])
            version = _id(spec["version"])
            if (spec.get("approval") != "approved" or
                    model not in spec.get("models", []) or
                    firmware not in spec.get("firmware", []) or
                    spec.get("architecture") != architecture or spec.get("abi") != abi):
                raise ModError("unapproved or incompatible mod")
            package = Path(package)
            expected = _digest(spec["sha256"])
            if hashlib.sha256(package.read_bytes()).hexdigest() != expected:
                raise ModError("package checksum mismatch")
            entry = spec.get("entrypoint")
            if not isinstance(entry, str) or not entry or entry.startswith("/") or "\\" in entry or any(p in (".", "..") for p in Path(entry).parts):
                raise ModError("invalid entrypoint")
            state = copy.deepcopy(self.read())
            for conflict in spec.get("conflicts", []):
                if _id(conflict) in state["mods"]:
                    raise ModError("conflicting installed mod")
            for dep in spec.get("dependencies", []):
                if _id(dep["id"]) not in state["mods"] or state["mods"][dep["id"]]["version"] != dep["version"]:
                    raise ModError("missing mod dependency")
            keys = []
            for dep in spec.get("libraries", []):
                if dep["architecture"] != architecture or dep["abi"] != abi:
                    raise ModError("library ABI mismatch")
                key = self.library_key(dep)
                if key not in state["libraries"]:
                    # Library payloads must be provisioned and verified by a separate
                    # trusted library installer; never treat a mod archive as a library.
                    raise ModError("library not provisioned")
                keys.append(key)
            target = self.root / "mods" / "installed" / mid / version
            if target.exists():
                raise ModError("package version already present")
            staging = Path(tempfile.mkdtemp(prefix="mod-", dir=str(self._staging())))
            try:
                safe_extract(package, staging)
                if not (staging / entry).is_file() or (staging / entry).is_symlink():
                    raise ModError("entrypoint missing")
                target.parent.mkdir(parents=True, exist_ok=True)
                os.replace(staging, target)
                previous = state["mods"].get(mid)
                state["mods"][mid] = {"version": version, "enabled": False,
                    "entrypoint": entry, "libraries": keys,
                    "dependencies": spec.get("dependencies", []),
                    "rollback": previous, "packageSha256": expected}
                self._rebuild_consumers(state)
                try:
                    self._commit(state)
                except Exception:
                    shutil.rmtree(target)
                    raise
            finally:
                if staging.exists():
                    shutil.rmtree(staging)

    def _staging(self):
        path = self.root / "mods" / "staging"
        path.mkdir(parents=True, exist_ok=True)
        return path

    def provision_library(self, spec, archive):
        """Verified library packages have no activation hooks."""
        with _LOCK:
            key = self.library_key(spec)
            if hashlib.sha256(Path(archive).read_bytes()).hexdigest() != _digest(spec["sha256"]):
                raise ModError("library checksum mismatch")
            state = copy.deepcopy(self.read())
            if key in state["libraries"]:
                return key
            dest = self.root / "libs" / spec["id"] / spec["version"] / (spec["architecture"] + "-" + spec["abi"] + "-" + spec["sha256"])
            staging = Path(tempfile.mkdtemp(prefix="lib-", dir=str(self._staging())))
            try:
                safe_extract(archive, staging)
                dest.parent.mkdir(parents=True, exist_ok=True)
                os.replace(staging, dest)
                state["libraries"][key] = {"path": str(dest.relative_to(self.root)), "consumers": []}
                self._commit(state)
            finally:
                if staging.exists():
                    shutil.rmtree(staging)
            return key

    @staticmethod
    def _rebuild_consumers(state):
        for lib in state["libraries"].values():
            lib["consumers"] = []
        for mid, mod in state["mods"].items():
            for key in mod.get("libraries", []):
                state["libraries"][key]["consumers"].append(mid)
        for lib in state["libraries"].values():
            lib["consumers"].sort()

    def enable(self, mid, enabled=True):
        with _LOCK:
            state = copy.deepcopy(self.read())
            if _id(mid) not in state["mods"]:
                raise ModError("mod not installed")
            state["mods"][mid]["enabled"] = bool(enabled)
            self._commit(state)

    def rollback(self, mid):
        with _LOCK:
            state = copy.deepcopy(self.read())
            mid = _id(mid)
            current = state["mods"].get(mid)
            if not current or not current.get("rollback"):
                raise ModError("no rollback version available")
            previous = current["rollback"]
            state["mods"][mid] = {**previous, "rollback": {k: v for k, v in current.items() if k != "rollback"}}
            self._rebuild_consumers(state)
            self._commit(state)

    def uninstall(self, mid):
        with _LOCK:
            state = copy.deepcopy(self.read())
            mid = _id(mid)
            if mid not in state["mods"]:
                raise ModError("mod not installed")
            if any(any(d["id"] == mid for d in m.get("dependencies", []))
                   for name, m in state["mods"].items() if name != mid):
                raise ModError("mod has installed dependents")
            old = state["mods"].pop(mid)
            self._rebuild_consumers(state)
            self._commit(state)
            # Only delete exact owned package directories; user data is untouched.
            location = self.root / "mods" / "installed" / mid / old["version"]
            if location.is_dir() and not location.is_symlink():
                shutil.rmtree(location)

    def garbage_collect(self, in_use=None):
        """Caller must supply keys in use by running processes; no blind GC."""
        if in_use is None:
            raise ModError("runtime usage check required")
        with _LOCK:
            state = copy.deepcopy(self.read())
            retained = {key for mod in state["mods"].values()
                        for key in (mod.get("rollback") or {}).get("libraries", [])}
            removed = []
            for key, lib in list(state["libraries"].items()):
                if not lib["consumers"] and key not in retained and key not in in_use:
                    path = self.root / lib["path"]
                    if path.is_symlink() or not path.resolve().is_relative_to((self.root / "libs").resolve()):
                        raise ModError("invalid managed library path")
                    removed.append((key, path))
                    del state["libraries"][key]
            self._commit(state)
            for key, path in removed:
                if path.is_dir():
                    shutil.rmtree(path)
            return [key for key, _ in removed]

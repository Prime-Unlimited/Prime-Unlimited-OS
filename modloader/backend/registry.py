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
    seen = set()
    with zipfile.ZipFile(archive) as source:
        for member in source.infolist():
            name = member.filename
            parts = Path(name).parts
            mode = member.external_attr >> 16
            kind = stat.S_IFMT(mode)
            if (not name or name.startswith("/") or "\\" in name or
                    any(p in ("", ".", "..") for p in parts) or
                    ":" in parts[0] or name in seen or
                    kind not in (0, stat.S_IFREG, stat.S_IFDIR)):
                raise ModError("unsafe archive member")
            seen.add(name)
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
        self.root.mkdir(parents=True, exist_ok=True)
        self.recover()

    def _empty(self):
        return {"schemaVersion": 1, "mods": {}, "libraries": {}}

    def read(self):
        if not self.registry.exists():
            return self._empty()
        state = json.loads(self.registry.read_text(encoding="utf-8"))
        if state.get("schemaVersion") != 1:
            raise ModError("unknown registry format")
        return state

    def recover(self):
        with _LOCK:
            if self.journal.exists():
                pending = json.loads(self.journal.read_text(encoding="utf-8"))
                # Roll forward after a crash; operations are idempotent snapshots.
                _atomic(self.registry, pending["next"])
                self.journal.unlink()
            self._startup(self.read())

    def _startup(self, state):
        enabled = []
        for mid, mod in sorted(state["mods"].items()):
            if mod.get("enabled") and mod.get("entrypoint"):
                enabled.append({"id": mid, "version": mod["version"],
                                "entrypoint": mod["entrypoint"],
                                "libraries": list(mod.get("libraries", []))})
        _atomic(self.startup, {"schemaVersion": 1, "enabled": enabled})

    def _commit(self, state):
        _atomic(self.journal, {"next": state})
        _atomic(self.registry, state)
        self._startup(state)
        self.journal.unlink()

    def libraries(self):
        state = self.read()
        return [{"key": key, **value} for key, value in sorted(state["libraries"].items())]

    def installed(self):
        return self.read()["mods"]

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

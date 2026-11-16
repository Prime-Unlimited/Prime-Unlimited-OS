"""Transactional package manager operating only inside a configured root."""
import hashlib
import os
import shutil
import tempfile
import time
import urllib.parse
import urllib.request
import uuid
from pathlib import Path

from registry import ModError, Registry, _atomic, _digest, _id
from resolver import Resolver

MAX_PACKAGE_BYTES = 128 * 1024 * 1024


class PackageManager:
    def __init__(self, root, catalog, device, downloader=None, trusted=False,
                 failure_injector=None):
        self.registry = Registry(root)
        self.root = self.registry.root
        self.catalog = catalog
        self.device = dict(device)
        self.downloader = downloader or self._download_https
        self.trusted = bool(trusted)
        self.failure_injector = failure_injector or (lambda stage: None)
        self.recover()

    @staticmethod
    def _download_https(url, destination):
        parsed = urllib.parse.urlsplit(url)
        if parsed.scheme != "https" or not parsed.netloc or parsed.username or parsed.password:
            raise ModError("catalog package URL must be HTTPS")
        request = urllib.request.Request(url, headers={"User-Agent": "Prime-Unlimited-Modloader/1"})
        total = 0
        with urllib.request.urlopen(request, timeout=20) as response, destination.open("xb") as target:
            while True:
                block = response.read(1024 * 1024)
                if not block:
                    break
                total += len(block)
                if total > MAX_PACKAGE_BYTES:
                    raise ModError("package exceeds size limit")
                target.write(block)
            target.flush()
            os.fsync(target.fileno())

    def _tx(self, operation, subject):
        txid = uuid.uuid4().hex
        record = {"id": txid, "operation": operation, "subject": subject,
                  "status": "running", "stage": "created", "createdAt": int(time.time())}
        path = self.registry.transactions / (txid + ".json")
        _atomic(path, record)
        return path, record

    def _stage(self, path, record, stage, **extra):
        record.update(extra)
        record["stage"] = stage
        _atomic(path, record)
        self.failure_injector(stage)

    def _finish(self, path, record, status, error=None):
        record.update(status=status, completedAt=int(time.time()))
        if error:
            record["error"] = str(error)
        _atomic(path, record)

    def recover(self):
        """Remove abandoned transaction scratch; Registry recovers snapshots."""
        for record in self.registry.transaction_status():
            if record.get("status") != "running":
                continue
            scratch = record.get("scratch")
            if scratch:
                path = (self.root / scratch).resolve()
                transaction_root = (self.root / "transactions").resolve()
                if path.is_relative_to(transaction_root) and path.is_dir() and not path.is_symlink():
                    shutil.rmtree(path)
            record.update(status="recovered", stage="rolled-back", completedAt=int(time.time()))
            _atomic(self.registry.transactions / (record["id"] + ".json"), record)

    def _catalog_record(self, kind, ident, version=None):
        candidates = [x for x in self.catalog.get(kind, []) if x.get("id") == ident]
        if version:
            candidates = [x for x in candidates if x.get("version") == version]
        if not candidates:
            raise ModError("catalog record not found")
        return sorted(candidates, key=lambda x: x["version"], reverse=True)[0]

    def _fetch(self, record, destination):
        url = record.get("downloadUrl")
        expected = _digest(record.get("sha256"))
        if not isinstance(url, str):
            raise ModError("missing package URL")
        self.downloader(url, destination)
        if not destination.is_file() or destination.stat().st_size > MAX_PACKAGE_BYTES:
            raise ModError("package exceeds size limit")
        digest = hashlib.sha256()
        with destination.open("rb") as stream:
            for block in iter(lambda: stream.read(1024 * 1024), b""):
                digest.update(block)
        actual = digest.hexdigest()
        if actual != expected:
            raise ModError("package checksum mismatch")

    def install(self, ident, version="*"):
        _id(ident)
        if not self.trusted:
            raise ModError("mutation disabled: trusted catalog authenticity not established")
        with self.registry.mutation_lock():
            path, tx = self._tx("install", ident)
            original = self.registry.read()
            scratch = Path(tempfile.mkdtemp(prefix=tx["id"] + "-", dir=str(self._transaction_root())))
            tx["scratch"] = str(scratch.relative_to(self.root))
            try:
                self._stage(path, tx, "trust-validated")
                resolution = Resolver(self.catalog, self.device).resolve(
                    [{"id": ident, "version": version}], self.registry.installed())
                self._stage(path, tx, "dependencies-resolved",
                            order=[list(x) for x in resolution.order])
                required = sum(int(x.get("size", 0)) for x in resolution.mods + resolution.libraries)
                if required and shutil.disk_usage(self.root).free < required * 2:
                    raise ModError("insufficient disk space")
                self._stage(path, tx, "space-checked")
                packages = {}
                for kind, records in (("library", resolution.libraries), ("mod", resolution.mods)):
                    for record in records:
                        key = (kind, record["id"], record["version"])
                        package = scratch / ("%s-%s-%s.zip" % key)
                        self._fetch(record, package)
                        packages[key] = package
                self._stage(path, tx, "downloaded")
                # Resolver order is dependencies first. Library provisioning is
                # idempotent and graph ownership is rebuilt when mods commit.
                for kind, depid in resolution.order:
                    record = next(x for x in (resolution.libraries if kind == "library" else resolution.mods)
                                  if x["id"] == depid)
                    package = packages[(kind, depid, record["version"])]
                    if kind == "library":
                        self.registry.provision_library(record, package)
                        self._stage(path, tx, "library-installed", packageId=depid)
                    else:
                        if depid in self.registry.installed() and self.registry.installed()[depid]["version"] == record["version"]:
                            continue
                        chosen_mods = {x["id"]: x for x in resolution.mods}
                        chosen_libs = {x["id"]: x for x in resolution.libraries}
                        spec = {**record, "approval": "approved",
                                "models": record.get("models", []),
                                "firmware": record.get("firmware", [self.device["firmware"]]),
                                "dependencies": [
                                    {"id": d["id"], "version": chosen_mods[d["id"]]["version"]}
                                    for d in record.get("dependencies", [])
                                    if d.get("type", "mod") == "mod"],
                                "libraries": [chosen_libs[d["id"]]
                                              for d in record.get("libraries", [])]}
                        self.registry.install(spec, package, self.device["model"],
                                              self.device["firmware"], self.device["architecture"],
                                              self.device["abi"])
                        self._stage(path, tx, "mod-installed", packageId=depid)
                self._stage(path, tx, "registry-committed")
                self._finish(path, tx, "completed")
                return tx["id"]
            except Exception as exc:
                self._restore_snapshot(original)
                self._finish(path, tx, "failed", exc)
                raise
            finally:
                if scratch.exists():
                    shutil.rmtree(scratch)

    def _transaction_root(self):
        path = self.root / "transactions"
        path.mkdir(parents=True, exist_ok=True)
        return path

    def _restore_snapshot(self, original):
        """Restore graph first, then remove only package versions introduced by this transaction."""
        current = self.registry.read()
        if current != original:
            self.registry._commit(original)
        for mid, mod in current["mods"].items():
            if mid not in original["mods"] or original["mods"][mid].get("version") != mod.get("version"):
                path = self.root / "mods/installed" / mid / mod["version"]
                if path.is_dir() and not path.is_symlink():
                    shutil.rmtree(path)
        for key, library in current["libraries"].items():
            if key not in original["libraries"]:
                path = (self.root / library["path"]).resolve()
                if path.is_relative_to((self.root / "libs").resolve()) and path.is_dir() and not path.is_symlink():
                    shutil.rmtree(path)

    def update(self, ident, version="*"):
        if ident not in self.registry.installed():
            raise ModError("mod not installed")
        return self.install(ident, version)

    def enable(self, ident):
        with self.registry.mutation_lock():
            self.registry.enable(ident, True)

    def disable(self, ident):
        with self.registry.mutation_lock():
            self.registry.enable(ident, False)

    def uninstall(self, ident, coordinated=False):
        with self.registry.mutation_lock():
            dependents = self.registry.dependents(ident)
            if dependents and not coordinated:
                raise ModError("mod is required by: " + ", ".join(dependents))
            if coordinated:
                for dependent in dependents:
                    self.registry.uninstall(dependent)
            self.registry.uninstall(ident)
            versions = self.root / "mods/installed" / ident
            if versions.is_dir() and not versions.is_symlink():
                shutil.rmtree(versions)

    def rollback(self, ident):
        with self.registry.mutation_lock():
            self.registry.rollback(ident)

    def list_installed(self):
        return self.registry.installed()

    def list_libraries(self):
        return self.registry.libraries()

    def list_dependents(self, ident):
        return self.registry.dependents(ident)

    def get_transaction_status(self, txid=None):
        return self.registry.transaction_status(txid)

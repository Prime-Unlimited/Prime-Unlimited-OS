#!/usr/bin/env python3
"""Loopback catalog and authenticated package-manager API."""
import argparse
import hmac
import json
import re
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import urlsplit

from manager import PackageManager
from registry import ModError

ID_RE = re.compile(r"^[a-z0-9][a-z0-9._-]{0,63}$")
SHA_RE = re.compile(r"^[a-fA-F0-9]{64}$")
MAX_CATALOG_BYTES = 1024 * 1024
MAX_REQUEST_BYTES = 16 * 1024


def load_catalog(path):
    if path.stat().st_size > MAX_CATALOG_BYTES:
        raise ValueError("catalog too large")
    data = json.loads(path.read_text(encoding="utf-8"))
    if not isinstance(data, dict) or data.get("schemaVersion") not in (1, 2) or not isinstance(data.get("mods"), list):
        raise ValueError("unsupported catalog")
    if data.get("schemaVersion") == 2 and not data.get("catalogId"):
        raise ValueError("missing trusted catalog identity")
    ids = set()
    for mod in data["mods"]:
        if not isinstance(mod, dict) or not ID_RE.fullmatch(str(mod.get("id", ""))):
            raise ValueError("invalid mod id")
        unique = (mod["id"], mod.get("version"))
        if unique in ids:
            raise ValueError("duplicate mod version")
        ids.add(unique)
        if not all(isinstance(mod.get(k), str) and mod[k] for k in ("name", "version", "downloadUrl")):
            raise ValueError("invalid mod metadata")
        if not mod["downloadUrl"].startswith("https://") or not SHA_RE.fullmatch(str(mod.get("sha256", ""))):
            raise ValueError("invalid download metadata")
        if not isinstance(mod.get("models"), list) or not mod["models"]:
            raise ValueError("invalid compatibility metadata")
        if not (mod.get("firmwareConstraint") or (isinstance(mod.get("firmware"), list) and mod["firmware"])):
            raise ValueError("invalid compatibility metadata")
    return data


def compatible(mod, model, firmware, architecture=None, abi=None):
    if model not in mod["models"]:
        return False
    if firmware not in mod.get("firmware", []) and not mod.get("firmwareConstraint"):
        return False
    return (architecture is None or mod.get("architecture") in (None, architecture)) and \
           (abi is None or mod.get("abi") in (None, abi)) and not mod.get("revoked", False)


def make_handler(catalog, model, firmware, manager=None, auth_token=None,
                 architecture=None, abi=None):
    class Handler(BaseHTTPRequestHandler):
        def log_message(self, fmt, *args):
            return

        def respond(self, status, payload):
            body = json.dumps(payload, separators=(",", ":"), ensure_ascii=False).encode("utf-8")
            self.send_response(status)
            self.send_header("Content-Type", "application/json; charset=utf-8")
            self.send_header("Content-Length", str(len(body)))
            self.send_header("Cache-Control", "no-store")
            self.send_header("X-Content-Type-Options", "nosniff")
            self.end_headers()
            self.wfile.write(body)

        def parsed_path(self):
            parsed = urlsplit(self.path)
            if parsed.query or parsed.fragment:
                raise ModError("unsupported query")
            return parsed.path

        def body(self):
            raw_length = self.headers.get("Content-Length")
            if raw_length is None or not raw_length.isdigit():
                raise ModError("content length required")
            length = int(raw_length)
            if length > MAX_REQUEST_BYTES:
                raise ModError("request too large")
            if self.headers.get("Content-Type", "").split(";", 1)[0] != "application/json":
                raise ModError("application/json required")
            data = json.loads(self.rfile.read(length).decode("utf-8"))
            if not isinstance(data, dict):
                raise ModError("JSON object required")
            return data

        def authorized(self):
            if manager is None or not auth_token:
                return False
            expected = "Bearer " + auth_token
            return hmac.compare_digest(self.headers.get("Authorization", "").encode(), expected.encode())

        def do_GET(self):
            try:
                path = self.parsed_path()
                if path == "/api/v1/health":
                    self.respond(200, {"service": "prime-unlimited-mod-loader",
                                       "mode": "authenticated" if manager and auth_token else "read-only"})
                elif path == "/api/v1/mods":
                    self.respond(200, {"model": model, "firmware": firmware,
                        "mods": [{**m, "compatible": compatible(m, model, firmware, architecture, abi)}
                                 for m in catalog["mods"]]})
                elif path == "/api/v1/installed" and manager:
                    self.respond(200, {"mods": manager.list_installed()})
                elif path == "/api/v1/libraries" and manager:
                    self.respond(200, {"libraries": manager.list_libraries()})
                elif path == "/api/v1/transactions" and manager:
                    self.respond(200, {"transactions": manager.get_transaction_status()})
                elif path.startswith("/api/v1/mods/") and manager:
                    ident = path.rsplit("/", 1)[-1]
                    if not ID_RE.fullmatch(ident):
                        raise ModError("invalid mod id")
                    records = [m for m in catalog["mods"] if m["id"] == ident]
                    installed = manager.list_installed().get(ident)
                    if not records and not installed:
                        self.respond(404, {"error": "not found"})
                    else:
                        self.respond(200, {"id": ident, "available": records,
                                           "installed": installed,
                                           "dependents": manager.list_dependents(ident)})
                elif path.startswith("/api/v1/transactions/") and manager:
                    status = manager.get_transaction_status(path.rsplit("/", 1)[-1])
                    self.respond(200 if status else 404, status or {"error": "not found"})
                else:
                    self.respond(404, {"error": "not found"})
            except (ModError, ValueError, json.JSONDecodeError) as exc:
                self.respond(400, {"error": str(exc)})

        def do_POST(self):
            if not self.authorized():
                self.respond(401, {"error": "mutation authorization required"})
                return
            try:
                path, data = self.parsed_path(), self.body()
                if any(key not in {"id", "version", "coordinated"} for key in data):
                    raise ModError("unexpected request field")
                ident = data.get("id")
                if not isinstance(ident, str) or not ID_RE.fullmatch(ident):
                    raise ModError("invalid mod id")
                if path == "/api/v1/install":
                    self.respond(202, {"transactionId": manager.install(ident, data.get("version", "*"))})
                elif path == "/api/v1/update":
                    self.respond(202, {"transactionId": manager.update(ident, data.get("version", "*"))})
                elif path == "/api/v1/enable":
                    manager.enable(ident); self.respond(200, {"ok": True})
                elif path == "/api/v1/disable":
                    manager.disable(ident); self.respond(200, {"ok": True})
                elif path == "/api/v1/uninstall":
                    manager.uninstall(ident, data.get("coordinated") is True); self.respond(200, {"ok": True})
                elif path == "/api/v1/rollback":
                    manager.rollback(ident); self.respond(200, {"ok": True})
                else:
                    self.respond(404, {"error": "not found"})
            except (ModError, ValueError, json.JSONDecodeError, KeyError) as exc:
                self.respond(400, {"error": str(exc)})

        def do_PUT(self):
            self.respond(405, {"error": "method not allowed"})

        def do_DELETE(self):
            self.respond(405, {"error": "method not allowed"})
    return Handler


def main():
    p = argparse.ArgumentParser(description="Prime Unlimited Mod Loader API")
    p.add_argument("--catalog", required=True, type=Path)
    p.add_argument("--model", required=True); p.add_argument("--firmware", required=True)
    p.add_argument("--architecture"); p.add_argument("--abi"); p.add_argument("--root", type=Path)
    p.add_argument("--auth-token-file", type=Path); p.add_argument("--trusted-catalog", action="store_true")
    p.add_argument("--port", type=int, default=8098)
    args = p.parse_args()
    if not 1 <= args.port <= 65535:
        p.error("invalid port")
    catalog = load_catalog(args.catalog)
    manager = None; token = None
    if args.root and args.auth_token_file and args.trusted_catalog:
        token = args.auth_token_file.read_text(encoding="utf-8").strip()
        if len(token) < 32:
            p.error("authorization token must contain at least 32 characters")
        manager = PackageManager(args.root, catalog,
            {"model": args.model, "firmware": args.firmware,
             "architecture": args.architecture, "abi": args.abi}, trusted=True)
    server = ThreadingHTTPServer(("127.0.0.1", args.port),
        make_handler(catalog, args.model, args.firmware, manager, token, args.architecture, args.abi))
    print("Prime Unlimited Mod Loader: http://127.0.0.1:%d" % args.port, flush=True)
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        server.server_close()


if __name__ == "__main__":
    main()

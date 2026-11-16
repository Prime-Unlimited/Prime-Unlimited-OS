#!/usr/bin/env python3
"""Prime Unlimited Mod Loader: offline, read-only loopback API (prototype)."""
import argparse
import json
import re
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import urlsplit

ID_RE = re.compile(r"^[a-z0-9][a-z0-9._-]{0,63}$")
SHA_RE = re.compile(r"^[a-fA-F0-9]{64}$")
MAX_CATALOG_BYTES = 1024 * 1024


def load_catalog(path):
    if path.stat().st_size > MAX_CATALOG_BYTES:
        raise ValueError("catalog too large")
    data = json.loads(path.read_text(encoding="utf-8"))
    if not isinstance(data, dict) or data.get("schemaVersion") != 1 or not isinstance(data.get("mods"), list):
        raise ValueError("unsupported catalog")
    ids = set()
    for mod in data["mods"]:
        if not isinstance(mod, dict) or not ID_RE.fullmatch(str(mod.get("id", ""))):
            raise ValueError("invalid mod id")
        if mod["id"] in ids:
            raise ValueError("duplicate mod id")
        ids.add(mod["id"])
        if not all(isinstance(mod.get(k), str) and mod[k] for k in ("name", "version", "downloadUrl")):
            raise ValueError("invalid mod metadata")
        if not mod["downloadUrl"].startswith("https://") or not SHA_RE.fullmatch(str(mod.get("sha256", ""))):
            raise ValueError("invalid download metadata")
        if not all(isinstance(mod.get(k), list) and mod[k] and all(isinstance(v, str) and v for v in mod[k]) for k in ("models", "firmware")):
            raise ValueError("invalid compatibility metadata")
    return data


def compatible(mod, model, firmware):
    # Explicit exact matches only: never guess device support.
    return model in mod["models"] and firmware in mod["firmware"]


def make_handler(catalog, model, firmware):
    class Handler(BaseHTTPRequestHandler):
        def respond(self, status, payload):
            body = json.dumps(payload, separators=(",", ":"), ensure_ascii=False).encode("utf-8")
            self.send_response(status)
            self.send_header("Content-Type", "application/json; charset=utf-8")
            self.send_header("Content-Length", str(len(body)))
            self.send_header("Cache-Control", "no-store")
            self.send_header("X-Content-Type-Options", "nosniff")
            self.end_headers()
            self.wfile.write(body)

        def do_GET(self):
            path = urlsplit(self.path)
            if path.query or path.fragment:
                self.respond(400, {"error": "unsupported query"})
            elif path.path == "/api/v1/health":
                self.respond(200, {"service": "prime-unlimited-mod-loader", "mode": "read-only"})
            elif path.path == "/api/v1/mods":
                self.respond(200, {
                    "model": model, "firmware": firmware,
                    "mods": [{**m, "compatible": compatible(m, model, firmware)} for m in catalog["mods"]]
                })
            else:
                self.respond(404, {"error": "not found"})

        def do_POST(self):
            self.respond(405, {"error": "read-only prototype"})

        def do_PUT(self):
            self.respond(405, {"error": "read-only prototype"})

        def do_DELETE(self):
            self.respond(405, {"error": "read-only prototype"})

    return Handler


def main():
    p = argparse.ArgumentParser(description="Offline read-only mod catalog API")
    p.add_argument("--catalog", required=True, type=Path)
    p.add_argument("--model", required=True)
    p.add_argument("--firmware", required=True)
    p.add_argument("--port", type=int, default=8098)
    args = p.parse_args()
    if not 1 <= args.port <= 65535:
        p.error("invalid port")
    catalog = load_catalog(args.catalog)
    server = ThreadingHTTPServer(("127.0.0.1", args.port), make_handler(catalog, args.model, args.firmware))
    print(f"Prime Unlimited Mod Loader: http://127.0.0.1:{args.port} (read-only)", flush=True)
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        server.server_close()


if __name__ == "__main__":
    main()

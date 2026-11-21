"""Command line for the mod release pipeline."""

import argparse
import json
import os
import sys
from pathlib import Path

from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey

from .pipeline import (
    DEVICES,
    REPO,
    GitHubAPI,
    ModReleaseError,
    activate_files,
    build_catalogs,
    catalog_bytes,
    ensure_asset,
    promote,
    publish_lock,
    require_review_gate,
    require_sign_environment,
    sign_catalog,
    validate_tree,
    verify_catalog,
)


def _repo(value: str) -> Path:
    return Path(value).resolve()


def _trust(root: Path) -> tuple[str, str]:
    document = json.loads((root / "modloader" / "catalog" / "trust" / "prime-unlimited-2026-1.json").read_text(encoding="utf-8"))
    if document.get("keyId") != "prime-unlimited-2026-1" or document.get("algorithm") != "ed25519":
        raise ModReleaseError("catalog trust key metadata does not match the existing trust chain")
    return document["keyId"], document["publicKey"]


def _private_key_from_env():
    raw = os.environ.get("CATALOG_ED25519_PRIVATE", "").strip()
    if not raw:
        raise ModReleaseError("CATALOG_ED25519_PRIVATE is not configured")
    try:
        seed = bytes.fromhex(raw)
    except ValueError as exc:
        raise ModReleaseError("CATALOG_ED25519_PRIVATE is not configured") from exc
    if len(seed) != 32 and len(seed) != 64:
        raise ModReleaseError("CATALOG_ED25519_PRIVATE is not configured")
    return Ed25519PrivateKey.from_private_bytes(seed[:32])


def cmd_validate(args) -> None:
    found = validate_tree(_repo(args.root))
    print(f"validated {len(found)} mod versions")


def cmd_render(args) -> None:
    root = _repo(args.root)
    with publish_lock(root):
        catalogs = build_catalogs(root)
        pairs = []
        for device, document in catalogs.items():
            destination = root / "modloader" / "catalog" / f"{device}.json"
            pairs.append((destination, catalog_bytes(document)))
        if args.check:
            for destination, payload in pairs:
                current = destination.read_bytes() if destination.exists() else b""
                if current != payload:
                    raise ModReleaseError(f"{destination.name} is not generated from mods/")
            print("catalogs match mods/")
            return
        activate_files(pairs, lambda _items: None)
    print("wrote device catalogs")


def cmd_sign(args) -> None:
    root = _repo(args.root)
    key_id, public_b64 = _trust(root)
    private = _private_key_from_env()
    from cryptography.hazmat.primitives import serialization
    import base64

    derived = private.public_key().public_bytes(serialization.Encoding.Raw, serialization.PublicFormat.Raw)
    if base64.b64encode(derived).decode("ascii") != public_b64:
        raise ModReleaseError("signing key does not match prime-unlimited-2026-1; refusing to replace the trust chain")
    pairs = []
    for device in ("jc11s", "jp24"):
        catalog = (root / "modloader" / "catalog" / f"{device}.json").read_bytes()
        signature = sign_catalog(catalog, private, key_id)
        verify_catalog(catalog, signature, public_b64)
        pairs.append((root / "modloader" / "catalog" / f"{device}.sig.json", signature))

    def verify(items):
        for destination, payload in items:
            device = destination.name.split(".")[0]
            catalog = (root / "modloader" / "catalog" / f"{device}.json").read_bytes()
            verify_catalog(catalog, payload, public_b64)

    with publish_lock(root):
        activate_files(pairs, verify)
    print("signed device catalogs")


def _write_release(mod, device, recorded) -> None:
    path = mod["directory"] / "release.json"
    document = mod["release"] if isinstance(mod["release"], dict) else {}
    assets = dict(document.get("assets") or {})
    assets[device] = recorded
    document["assets"] = assets
    document.setdefault("tag", f"mod-{mod['id']}-v{mod['version']}")
    path.write_text(json.dumps(document, indent=2) + "\n", encoding="utf-8")


def cmd_publish(args) -> None:
    """Validate development mods, bind immutable assets, then sign catalogs."""

    root = _repo(args.root)
    key_id, public_b64 = _trust(root)
    private = _private_key_from_env()
    from cryptography.hazmat.primitives import serialization
    import base64

    derived = private.public_key().public_bytes(serialization.Encoding.Raw, serialization.PublicFormat.Raw)
    if base64.b64encode(derived).decode("ascii") != public_b64:
        raise ModReleaseError("signing key does not match prime-unlimited-2026-1; refusing to replace the trust chain")
    api = None
    with publish_lock(root):
        mods = validate_tree(root)
        for mod in mods:
            if mod["channel"] != "development":
                continue
            for device in mod["targets"]:
                if mod["targets"][device]["asset"].get("id"):
                    print(f"reuse {mod['id']} {mod['version']} {device} asset {mod['targets'][device]['asset']['id']} sha256 {mod['targets'][device]['sha256']}")
                    continue
                if api is None:
                    api = GitHubAPI(os.environ.get("GH_TOKEN", ""))
                blob = (mod["directory"] / f"{device}.zip").read_bytes()
                tag = (mod["release"] or {}).get("tag") or f"mod-{mod['id']}-v{mod['version']}"
                recorded = ensure_asset(api, tag, f"{mod['id']}-v{mod['version']}-{device}.zip", blob)
                _write_release(mod, device, recorded)
                print(f"bound {mod['id']} {mod['version']} {device} asset {recorded['id']} sha256 {recorded['sha256']}")
        catalogs = build_catalogs(root)
        catalog_pairs = []
        signature_pairs = []
        for device, document in catalogs.items():
            payload = catalog_bytes(document)
            signature = sign_catalog(payload, private, key_id)
            verify_catalog(payload, signature, public_b64)
            catalog_pairs.append((root / "modloader" / "catalog" / f"{device}.json", payload))
            signature_pairs.append((root / "modloader" / "catalog" / f"{device}.sig.json", signature))

        def verify(items):
            bodies = {path.name: payload for path, payload in items}
            for device in DEVICES:
                verify_catalog(bodies[f"{device}.json"], bodies[f"{device}.sig.json"], public_b64)

        activate_files(catalog_pairs + signature_pairs, verify)
    print("development catalogs published")


def cmd_check_environment(args) -> None:
    import json
    import urllib.error
    import urllib.request

    name = args.environment
    token = os.environ.get("GH_TOKEN", "")
    if not token:
        raise ModReleaseError("GH_TOKEN is not configured")

    def fetch(url: str):
        request = urllib.request.Request(
            url,
            headers={
                "Accept": "application/vnd.github+json",
                "Authorization": f"Bearer {token}",
                "User-Agent": "Prime-Unlimited-Mod-Release",
                "X-GitHub-Api-Version": "2022-11-28",
            },
        )
        with urllib.request.urlopen(request, timeout=30) as response:
            return json.load(response)

    base = f"https://api.github.com/repos/{REPO}/environments/{name}"
    try:
        environment = fetch(base)
        policies = fetch(base + "/deployment-branch-policies")
    except urllib.error.HTTPError as exc:
        raise ModReleaseError(
            f"Environment {name} is not available (HTTP {exc.code}). Create it before publishing."
        ) from exc
    except urllib.error.URLError as exc:
        raise ModReleaseError(f"Environment {name} is not available. Create it before publishing.") from exc
    if name == "mod-sign":
        require_sign_environment(environment, policies)
    elif name == "mod-release":
        require_review_gate(environment, policies)
    else:
        raise ModReleaseError("unknown GitHub environment")
    print(f"{name} is configured")


def cmd_promote(args) -> None:
    root = _repo(args.root)
    digests = {}
    for item in args.sha:
        device, digest = item.split("=", 1)
        digests[device] = digest
    with publish_lock(root):
        destination = promote(root, args.mod, args.version, digests, args.run_id)
        catalogs = build_catalogs(root)
        pairs = [(root / "modloader" / "catalog" / f"{device}.json", catalog_bytes(document)) for device, document in catalogs.items()]
        activate_files(pairs, lambda _items: None)
    print(f"promoted {destination}")


def main(argv=None) -> int:
    parser = argparse.ArgumentParser(description="Prime Unlimited mod release pipeline")
    common = argparse.ArgumentParser(add_help=False)
    common.add_argument("--root", default=".")
    sub = parser.add_subparsers(dest="command", required=True)
    validate = sub.add_parser("validate", parents=[common])
    validate.set_defaults(func=cmd_validate)
    render = sub.add_parser("render", parents=[common])
    render.add_argument("--check", action="store_true")
    render.set_defaults(func=cmd_render)
    sign = sub.add_parser("sign", parents=[common])
    sign.set_defaults(func=cmd_sign)
    publish = sub.add_parser("publish-development", parents=[common])
    publish.set_defaults(func=cmd_publish)
    check = sub.add_parser("check-environment", parents=[common])
    check.add_argument("--environment", required=True, choices=("mod-sign", "mod-release"))
    check.set_defaults(func=cmd_check_environment)
    promote_parser = sub.add_parser("promote", parents=[common])
    promote_parser.add_argument("--mod", required=True)
    promote_parser.add_argument("--version", required=True)
    promote_parser.add_argument("--sha", action="append", required=True)
    promote_parser.add_argument("--run-id", required=True)
    promote_parser.set_defaults(func=cmd_promote)
    args = parser.parse_args(argv)
    try:
        args.func(args)
    except ModReleaseError as exc:
        print(f"mod release failed: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

# JC11S Modloader bootstrap in the installer

The JC11S `/data` image contains one deliberately pinned Modloader bootstrap.
`bootstrap/jc11s/modloader-bootstrap.json` records its version, revision,
release asset ID, byte size and SHA-256. The matching ZIP is committed beside
the pin, so Windows and macOS builds do not depend on a mutable download.
`core/jc11s_bootstrap.py` rejects a changed archive, unexpected ZIP entries or
a mismatched internal `VERSION.json` before an image is created.

The image also contains the signed JC11S catalog and trust root, the JC11S UI
hook, Control Center and About QML patches, and the second-stage splash. On the
first boot an idempotent script copies the stock Engine launch scripts into
`/data`, creates the preload wrappers there and invalidates only Engine's QML
cache. It never changes an OFW file or a firmware partition. A missing or
unsupported stock launcher leaves Engine untouched and is logged.

## Release policy

- `0.2.0-r8` is the one-time bootstrap used for the current installer.
- Minor, patch and revision releases are delivered by the signed Self-Updater.
  They do not change `bootstrap/jc11s/modloader-bootstrap.*` and therefore do
  not trigger desktop installer builds.
- For a future major (`1.0.0`, `2.0.0`, ...), publish and verify its signed
  JC11S update first. Replace the pinned ZIP and all pin fields together, run
  the bootstrap and image tests, and commit that explicit bootstrap bump on
  `main`. That change triggers new Windows and macOS installers.
- JP24 uses its existing path and is not part of this bootstrap policy yet.

The desktop workflow is scoped with `paths`. Ordinary changes below
`modloader/` do not build desktop installers; changes to the committed JC11S
bootstrap, image builder or installer code do. Manual builds remain available
through `workflow_dispatch`.

# Mod publishing contract

The device UI never talks to GitHub and never contains a GitHub credential. For a catalog item
whose `status` is `dev`, the UI shows **Publish** only while DEV MODE is enabled. It calls the
authenticated local endpoint `POST /api/v1/publish` with `{"id":"<mod-id>"}` and disables the
button until the request succeeds or fails.

The runtime implementation of that endpoint must ask a trusted service to dispatch
`publish-mod.yml` with `mod_id`, the detected `device`, and the repository `package_path`. It must
record a transaction with operation `publish`, poll the workflow, and expose `status`, `stage` and
`progress` through `GET /api/v1/transactions`. A failure marks the transaction `failed`; the UI
then releases the button. Success is only returned after the workflow has created the
device-specific release and pushed the newly signed catalog.

The same transaction fields drive normal Modloader progress:

- `cache` or `download` stage: download percentage.
- `stage`, `inspect`, `install`, or `update` stage: installation percentage.
- Self-updater `GET /api/v1/status`: numeric `progress` from 0 through 100 while busy.

Existing catalog entries without `status` are interpreted as `published` for backwards
compatibility. New entries must explicitly use `dev` or `published`.


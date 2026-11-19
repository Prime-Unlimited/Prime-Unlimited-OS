# Official catalog releases

The normal reusable mod workflow is deliberately secretless and read-only. A
matching release tag produces one Actions artifact named
`official-candidate-<mod-id>-<commit-sha>`. It contains both target ZIPs, their
checksums and sizes, and `release-candidate.json`. It does not create a GitHub
Release and cannot sign a catalog.

Official publication is manually dispatched through
`.github/workflows/official-mod-release.yml` from protected `main`. GitHub only
registers a `workflow_dispatch` workflow after the workflow file exists on the
default branch, so merging this work into the feature integration branch is not
enough to activate production signing. Merge the reviewed integration branch
into protected `main` before configuring or invoking the Environment. The dispatcher
must supply the exact tag, 40-character commit SHA, mod ID and successful
candidate workflow run ID. The workflow binds the run repository, event, tag,
commit, artifact name and GitHub-provided artifact SHA-256 before extracting
anything.

Every version requires a reviewed file at
`modloader/catalog/release-approvals/<mod-id>-<version>.json`. Build success is
mechanical evidence only. Technical compatibility, hardware verification and
official catalog approval are independent booleans for each target. Unknown,
partial or non-boolean approval fails closed. Unapproved targets remain in the
candidate matrix but are not inserted into their official device catalog.

Configure the protected GitHub Environment `prime-unlimited-release` with Luke
and dj.Mr.Brown as equal authorized reviewers, prevent self-review where the
plan supports it, and restrict deployment to the protected release branch.
Add `PRIME_UNLIMITED_CATALOG_SIGNING_KEY` as its only signing secret. Its value
is the existing `prime-unlimited-2026-1` 64-byte Ed25519 private key encoded as
exactly 128 lowercase hexadecimal characters. Never paste it into repository
files, workflow inputs, logs or artifacts.

The Environment job checks out only the exact protected workflow commit. It
does not check out or execute mod scripts, QML, entrypoints or ZIP payloads. It
recomputes candidate hashes and sizes, assembles with the existing general
assembler, signs exact catalog bytes with the existing signer, verifies each
signature independently, and byte-compares the derived public trust document
with the committed `prime-unlimited-2026-1` trust root. The GitHub Release stays
draft until every expected package, checksum, size, manifest, catalog and
signature exists exactly once and round-trips byte for byte.

The implementation verifies GitHub's v4 Actions-artifact digest, exact run
provenance, and GitHub build-provenance attestations for both ZIPs, their
checksum/size inventories, and the candidate manifest. Attestation verification
is repeated inside the protected job and never silently skips when the service
is unavailable.

Use repository rulesets to protect the release branch and release tags, require
review of workflow/catalog/trust/approval changes, keep the default Actions
token read-only, and disallow workflow approval bypass. The fixed concurrency
group serializes all official catalog releases so parallel releases cannot
overwrite a shared base state. Configure a narrowly scoped ruleset exception
for this Environment-gated workflow's GitHub Actions identity to update only
the four canonical catalog/signature files in one atomic Git commit. The job
re-verifies the current signed bases and rejects a changed branch tip before
publication. It publishes the complete immutable Release before exposing its
catalog entries. If the later catalog push is rejected, the public packages are
harmlessly undiscoverable and an exact rerun resumes the push; it never leaves
an official catalog pointing at private draft assets.

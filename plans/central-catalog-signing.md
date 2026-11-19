# Primary goal

Luke (Bluecy) and dj.Mr.Brown can independently prepare official cross-device
mod releases, while one protected GitHub release path signs a preserved
official catalog with the existing `prime-unlimited-2026-1` trust root and
publishes only fully approved, reproducible artifacts.

## Phase 0: Documentation discovery and threat model

**How this serves the primary goal:** It anchors every release and security
decision in the formats already accepted by both devices and the controls
GitHub actually provides.

### What to record

- Copy the detached Ed25519 contract from
  `modloader/catalog/tools/sign_catalog.go` and the verification contract from
  `modloader/device/jc11s/runtime-api/main.go`.
- Record the private-key file contract: one 128-character lowercase hex Go
  Ed25519 private key, optional newline, regular file owned by the runner user,
  mode `0600`.
- Record the existing public trust document
  `modloader/catalog/trust/prime-unlimited-2026-1.json`; do not copy or inspect
  private key material.
- Copy the reusable matrix/release pattern from
  `.github/workflows/reusable-mod-release.yml` and the thin caller from
  `.github/workflows/prime-clock.yml`.
- Record official GitHub environment, ruleset, reusable-workflow, artifact
  attestation, concurrency, and untrusted-PR guidance in the release-security
  documentation.

### Allowed APIs

- Existing `sign_catalog.go` detached signature and trust JSON formats.
- Schema v2 and the current runtime's exact model/firmware/architecture/ABI
  matching rules.
- GitHub protected environments, required reviewers, protected tag rulesets,
  concurrency groups, immutable artifact digests, and attestations.

### Verification checklist

- Discovery report cites exact source paths and GitHub documentation.
- Gaps in live repository settings and Raspberry Pi credential path are
  explicitly marked manual rather than guessed.

### Anti-pattern guards

- No new trust root, signer format, embedded authenticity, or Prime Clock-only
  signing path.
- No private value in files, logs, command lines, artifacts, or plans.

## Phase 1: Reusable catalog assembly and approval gate

**How this serves the primary goal:** It makes deterministic, fail-closed
per-device catalog candidates from reusable release metadata without dropping
existing mods or approving an untested platform.

### What to implement

- Add a general catalog release tool under `modloader/catalog/tools/` that:
  - loads the applicable canonical schema-v2 device catalog (`jc11s.json` or
    `jp24.json`) and verifies its device `catalogId`;
  - loads a release manifest describing final ZIP names, SHA-256, size, HTTPS
    release URLs, and target compatibility;
  - requires explicit per-target approval fields matching exact runtime
    identity and nonempty firmware/Qt requirements;
  - rejects missing, unknown, incomplete, duplicate, revoked-conflicting, or
    unapproved target records;
  - replaces only the same stable target-specific mod ID, preserving every
    unrelated mod/library and all dependency/conflict/update/revocation fields;
  - verifies an expected base-catalog SHA-256 before writing;
  - enforces the runtime `revision` update contract and writes deterministic
    exact bytes atomically.
- Preserve canonical per-device catalog/signature pairs and all existing
  entries without rewriting historical signed catalog files.
- Define a reusable release-manifest/approval format and document its state
  distinction: built, structurally valid, technically compatible, officially
  approved.

### Documentation references

- Strict v2 parser and duplicate guards:
  `modloader/device/jc11s/runtime-api/main.go` (`parseCatalog` and
  `loadVerifiedCatalog`).
- Compatibility fields: `modloader/catalog/schema-v2.json`.
- Existing immutable record:
  `modloader/catalog/prime-clock-v0.1.1-jc11s.json`.
- Hardware approval checklist: `modloader/OVERLAY_MOD_DEVELOPMENT.md`.

### Verification checklist

- Unit tests prove deterministic output and exact base-digest enforcement.
- Tests prove unrelated mods, libraries, dependencies, conflicts and
  revocations survive an update; unsupported `update` metadata fails closed
  because the strict runtime currently rejects it.
- Tests reject duplicate IDs, unknown target identity, empty firmware/Qt,
  unapproved target, wrong ZIP hash/size, incomplete matrix, and stale base.

### Anti-pattern guards

- Do not represent multiple versions with duplicate IDs; current runtime
  supports one active record per ID.
- Do not infer hardware approval from a successful 32-/64-bit build.
- Do not rebuild the catalog from only the newest mod fragment.

## Phase 2: Existing signer verification mode and test-key security suite

**How this serves the primary goal:** It proves that the exact catalog signed
inside GitHub is accepted by the same trust contract as both devices, without
creating a second cryptographic implementation.

### What to implement

- Extend `modloader/catalog/tools/sign_catalog.go` with a verification mode
  that reads the existing detached signature and trust JSON formats and
  verifies exact catalog bytes.
- Keep the current signing CLI compatible; production signing still uses
  `-key-id prime-unlimited-2026-1` and never `-generate`.
- Add automated tests using ephemeral test keys only for valid signature,
  tampered catalog, wrong key ID, wrong key, malformed signature, missing
  private key, and unsafe private-key permissions.
- Add package/catalog cross-check tests for a modified ZIP and false digest.

### Documentation references

- Key loader/output code: `modloader/catalog/tools/sign_catalog.go`.
- Runtime verification order: `modloader/device/jc11s/runtime-api/main.go`.
- Existing ephemeral-key test pattern:
  `modloader/device/jc11s/runtime-api/main_test.go`.

### Verification checklist

- `go test` passes for the catalog tools and runtime package.
- A test signature verifies; every tamper/wrong-ID case fails.
- Existing official catalog/signature/trust fixtures still verify unchanged.

### Anti-pattern guards

- No production private key in tests.
- No signature or secret output in test logs.
- No catalog reserialization between signing and verification.

## Phase 3: Isolated protected GitHub signing and publication

**How this serves the primary goal:** It lets either maintainer initiate the
same official process while keeping the shared private key behind explicit
review and preventing untrusted build code from receiving it.

### What to implement

- Extend the reusable build workflow to emit immutable package manifests and
  attest both target ZIPs; keep all mod scripts in secret-free jobs.
- Add a general protected signing/publication workflow sourced from the
  protected release branch, not a mod-specific workflow.
- Serialize all official catalog publication with repository-wide
  `concurrency`, `cancel-in-progress: false`.
- Before the environment gate, verify tag/version, approved commit ancestry,
  complete 32-/64-bit matrix, artifact attestations, ZIP hash/size, release
  manifest, current official catalog base digest, and target approval.
- In the only secret-bearing job:
  - use environment `prime-unlimited-release`;
  - grant minimum required permissions;
  - do not execute mod/package scripts or ZIP content;
  - materialize `PRIME_UNLIMITED_CATALOG_SIGNING_KEY` into a trap-cleaned
    mode-`0600` temporary file without echoing it;
  - sign with existing `sign_catalog.go`;
  - independently verify the signature;
  - compare the derived public trust document byte-for-byte with committed
    `prime-unlimited-2026-1.json`;
  - publish ZIPs, catalog, detached signature, checksums, and provenance to a
    draft release, verify completeness, then publish it.
- Re-read the current official catalog immediately before publish and reject a
  stale base even with concurrency enabled.

### Documentation references

- Reusable jobs/artifacts: `.github/workflows/reusable-mod-release.yml`.
- GitHub protected environments and deployment review documentation cited in
  `docs/RELEASE_SIGNING.md`.
- Existing signer invocation: `modloader/catalog/tools/README.md`.

### Verification checklist

- PR jobs never request the signing environment or secret.
- Workflow tests/fixtures reject wrong tag, missing secret, incomplete target
  matrix, unapproved target, stale catalog base, and tampered artifact.
- A test-key-only dry run produces and verifies the expected detached files.
- GitHub workflow syntax and all CI jobs pass.

### Anti-pattern guards

- Never use `pull_request_target`, broad `secrets: inherit`, mutable untrusted
  code in the secret job, or `-generate` in production.
- Do not rely on an environment secret as the only security boundary.
- Do not overwrite live catalog assets from a stale release run.

## Phase 4: Trust-root provisioning and maintainer operations

**How this serves the primary goal:** It ensures both devices trust the same
official catalog and both maintainers can operate, recover, and rotate the
release process without sharing local signing files.

### What to implement

- Copy only the committed public trust document into the JC11S and JP24
  install paths as `/data/prime-unlimited/trust/catalog-key.json`, with safe
  ownership/mode; never provision private material.
- Add `docs/RELEASE_SIGNING.md` covering:
  - equal Luke/Brownie release preparation and initiation;
  - manual creation of `prime-unlimited-release`;
  - environment reviewers, prevent-self-review, no admin bypass, protected
    tag deployment rules, branch/tag rulesets, required checks and CODEOWNERS;
  - exact secret format without its value;
  - migration of the existing Raspberry Pi key by an authorized operator,
    fingerprint/trust-root confirmation, and no guessed Pi path;
  - encrypted offline backup and restore validation;
  - compromise response and deliberate trust-root rotation;
  - invalid-signature troubleshooting;
  - distinction between package build, platform approval, catalog merge, and
    cryptographic signature.

### Documentation references

- Current custody and fingerprint evidence:
  `modloader/device/jc11s/runtime-api/VERIFIED_SIGNATURE_STATUS.md`.
- Device paths: both runtime `start.sh` files.
- GitHub official environment/ruleset/security documentation linked in the
  new guide.

### Verification checklist

- Both installers stage the same public trust JSON and correct safe modes.
- Documentation contains no secret value or guessed credential path.
- Manual settings checklist clearly separates code-complete from
  administrator-complete state.

### Anti-pattern guards

- Do not automate around missing GitHub permissions or approvals.
- Do not tell maintainers to exchange private key files after migration.
- Do not rotate the existing trust root during this task.

## Phase 5: End-to-end verification and guarded rollout

**How this serves the primary goal:** It demonstrates that the reusable system
rejects unsafe releases and produces a complete official release only when all
cryptographic, compatibility, and governance gates agree.

### What to verify

- Run all catalog tool, runtime, mod, and workflow validation tests.
- Run anti-pattern greps for private-key material, embedded authenticity,
  production `-generate`, `pull_request_target`, unsafe secret inheritance,
  and Prime Clock-specific signing logic.
- Open a PR against `feat/prime-unlimited-mod-loader`; inspect CI, review
  comments, mergeability, and unresolved threads until clean.
- Do not perform a production signature until the GitHub environment, secret,
  reviewers, protected branch/tag rules, and encrypted backup are manually
  confirmed.

### Verification checklist

- All automated security cases requested in the task are covered and passing.
- Existing signatures remain byte-for-byte untouched and valid.
- PR checks pass with zero unresolved actionable threads.
- Final report distinguishes repository implementation from pending external
  GitHub configuration and production-key migration.

### Anti-pattern guards

- No claim that production signing is active before manual controls exist.
- No merge of unreviewed security-sensitive workflow changes.

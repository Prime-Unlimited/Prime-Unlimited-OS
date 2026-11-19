# Official release signing operations

This document defines the human and repository controls for official Prime
Unlimited mod releases. It complements the enforced workflow contract in
[`modloader/catalog/OFFICIAL_RELEASES.md`](../modloader/catalog/OFFICIAL_RELEASES.md).

> **Not active yet:** production signing is not available merely because these
> files exist on a feature branch. The reviewed workflows must first be merged
> into protected `main`, and an administrator must complete every GitHub setting
> in this document. Until then, builds are candidates only and must not be
> described as official signed releases.

## Roles and equal access

Luke (Bluecy) and dj.Mr.Brown (Brownie) are equal maintainers of the official
release process. Either maintainer may prepare a mod version, open the release
PR, create the approved tag, start the candidate build, and dispatch the
official workflow. Neither maintainer owns a local-only production signing
step after migration.

Environment review is intentionally a second-person control. With prevent
self-review enabled, the maintainer who dispatches a release cannot approve
their own deployment; the other authorized maintainer reviews it. This means
both maintainers can independently **initiate** the same process, but neither
can unilaterally complete it. Do not weaken this control to work around
availability. Use the documented recovery process or postpone the release.

## Four different release decisions

These states are related but are not interchangeable:

1. **Package build** proves that deterministic JC11S and JP24 ZIPs were
   produced and structurally checked. It is not hardware approval.
2. **Platform approval** records technical compatibility, hardware
   verification, and explicit official-catalog approval independently for each
   target in
   `modloader/catalog/release-approvals/<mod-id>-<version>.json`. An AArch64
   build alone does not approve JP24.
3. **Catalog merge** preserves the existing official catalog and adds or
   updates only approved target records after checking the current base digest.
4. **Cryptographic signing** authenticates the exact merged catalog bytes with
   the official Ed25519 key. It does not establish hardware compatibility and
   it does not replace package digest verification.

## Normal release process

1. Prepare one shared mod source and update its version. Build and test both
   target variants through the reusable workflow.
2. Record target decisions in a reviewed release-approval file. Unknown or
   unconfirmed target values remain unapproved.
3. Merge the code, approval, workflow, and catalog-related changes through a
   protected pull request into `main`.
4. Create the exact protected release tag from a commit contained in `main`.
   The tag must match `<mod-id>-v<version>`.
5. Confirm that the successful candidate run belongs to this repository, tag,
   and commit and contains the complete JC11S/JP24 matrix.
6. From the `main` version of **Official protected mod release**, dispatch the
   workflow with the exact tag, 40-character commit SHA, mod ID, candidate run
   ID, and release title.
7. The other authorized maintainer reviews the Environment deployment. They
   verify the tag/commit, approval file, target decisions, candidate run, and
   expected catalog change before approving.
8. Confirm that the workflow verifies provenance and package inventories,
   signs with key ID `prime-unlimited-2026-1`, publishes the complete immutable
   Release, and then pushes one catalog commit containing both catalogs and
   both detached signatures.
9. If Release publication succeeds but the catalog push fails, rerun the exact
   dispatch after resolving the branch/ruleset problem. The recovery path
   requires byte-identical published assets and resumes only the verified
   catalog update.

Never publish a draft manually and never edit official assets to make a failed
run appear successful.

## GitHub Environment setup

An organization or repository administrator must create an Environment named
exactly `prime-unlimited-release` and configure it as follows:

- Add Luke and dj.Mr.Brown as equally authorized required reviewers.
- Enable **Prevent self-review**.
- Disable administrator bypass for deployment protections. If the GitHub plan
  exposes this as a repository-wide setting, enforce the equivalent rule
  there and record the setting in the administrative audit log.
- Restrict deployment branches to protected `main` only. Do not allow all
  branches or tags.
- Add exactly one signing secret named
  `PRIME_UNLIMITED_CATALOG_SIGNING_KEY`.
- Keep the normal repository `GITHUB_TOKEN` default at read-only; grant write
  permissions only to the explicit jobs already declared in the reviewed
  workflows.

The secret value format is exactly one lowercase hexadecimal Ed25519 private
key: **128 ASCII hexadecimal characters representing 64 bytes**, with no
quotes, spaces, prefix, or additional fields. A trailing newline in the GitHub
secret editor should be avoided. Never use `-generate` in production.

Do not place the private key in a repository secret, workflow input, issue,
pull request, Actions artifact, cache, device, shell history, chat, screenshot,
or documentation. Environment access is necessary but is not the only security
boundary; protected workflow source, review, attestations, exact hashes, and
branch rules remain mandatory.

## Branch, tag, and ownership rules

Create rulesets for `main` and official release tags before enabling the
Environment.

For `main`:

- Require pull requests, passing required checks, resolved review threads, and
  review of the latest push.
- Require at least one approval from someone other than the author. Require
  the other maintainer for release-sensitive changes.
- Block force pushes and deletion.
- Apply the rules to administrators and disallow bypass for normal releases.
- Restrict direct pushes. Add only the narrowly scoped GitHub Actions exception
  needed by the Environment-gated official workflow to push the four canonical
  catalog/signature files in one commit.
- Require review for changes under `.github/workflows/`,
  `modloader/catalog/tools/`, `modloader/catalog/release-approvals/`,
  `modloader/catalog/trust/`, the canonical catalogs/signatures, and this
  document.

For official tags:

- Protect the project release-tag patterns, including current mod tags such as
  `prime-clock-v*` and equivalent reviewed patterns for future mods.
- Restrict tag creation to the two maintainers or an explicitly managed
  release-maintainers team.
- Block tag update and deletion, and require the tagged commit to be contained
  in protected `main`.
- Do not permit a ruleset bypass merely because a user is an administrator.

Add `.github/CODEOWNERS` entries for all release-sensitive paths listed above.
Use the real GitHub handles for Luke and dj.Mr.Brown, or a team whose membership
contains both with equal rights. Do not guess handles in repository code.
Protect the CODEOWNERS file itself and enable required Code Owner review. Note
that GitHub normally requires approval from one listed owner; the Environment's
prevent-self-review rule supplies the mandatory second-person gate for actual
signing.

Enable immutable releases for the repository if the GitHub plan supports it.
Record all settings that cannot be represented in Git in the repository's
administrative checklist or audit record.

GitHub ruleset bypass is normally actor-scoped, not path-scoped. The exception
above therefore cannot by itself restrict an Actions identity to four files.
Treat it as repository-wide write authority and compensate with required review
of workflow changes, CODEOWNERS, read-only token defaults, the protected
Environment gate, and the workflow's explicit four-file diff/staging checks.
A narrowly permissioned GitHub App is preferable if stronger actor isolation is
required.

## Migrating the existing key

The existing private key remains with its current authorized custodian. Project
history says it is in protected Raspberry Pi credential storage, but this
document deliberately does not guess a device name or filesystem path. The
custodian must locate it using their existing authorized procedure.

Migration is performed once on a trusted, non-recorded administrative session:

1. Stop unrelated screen sharing, terminal logging, shell tracing, clipboard
   managers, and command-history capture. Confirm the source is a regular file
   owned by the authorized account with mode `0600`.
2. Do not copy the plaintext key through Git, SCP to a DJ device, chat, email,
   cloud-drive synchronization, or a temporary Actions artifact.
3. Using the existing `sign_catalog.go` tool in an isolated temporary directory,
   derive a temporary public trust document from the existing key by signing a
   harmless temporary catalog. Reuse the key; do **not** pass `-generate`.
4. Byte-compare the derived trust document with
   `modloader/catalog/trust/prime-unlimited-2026-1.json` and confirm the tool
   reports key ID `prime-unlimited-2026-1` and public-key SHA-256 fingerprint:

   ```text
   17bafd5349bc7338b09697ef346b3fd796b2af09aa7c54f82d0c68d95e6f1441
   ```

   Abort migration on any mismatch. Do not replace the committed trust root and
   do not create a new key to make the check pass.
5. The authorized custodian enters the existing key directly into the GitHub
   Environment secret editor over an authenticated GitHub session. No other
   maintainer needs or receives a plaintext copy.
6. Delete temporary catalog, signature, and derived trust files. Securely clear
   any unavoidable temporary plaintext according to the host's storage policy.
7. Run a test-key-only workflow validation first. Production signing remains
   disabled until branch rules, Environment review, and protected `main` are all
   active.

The public fingerprint is safe to compare and log. The private key and its
128-character encoding are not.

## Encrypted backup and restore drill

Maintain at least one encrypted offline backup before retiring any existing
custody system.

- Use organization-approved authenticated encryption with recovery material
  held separately from the encrypted backup. Prefer multiple authorized
  recipients or a documented two-person recovery process.
- Store redundant encrypted copies on offline media in separate controlled
  locations. Record creation date, key ID, cipher/tool version, custodians, and
  the public fingerprint, but never plaintext or a plaintext digest of the
  private key.
- Limit backup creation and restore to authorized custodians on a trusted
  machine. Temporary restored files must be owner-only regular files with mode
  `0600` and must never enter a synchronized folder.
- Perform a restore drill before declaring the backup valid. Derive the public
  trust document from the restored key, compare it byte-for-byte with the
  committed trust root, and confirm the fingerprint above. Then remove the
  restored plaintext.
- Test recovery periodically and after changing encryption tooling, recipients,
  or custodian access. A backup that has not passed a restore-and-fingerprint
  drill is not a usable backup.

Never validate a backup by exposing its private value in a command line, log,
CI job, or support conversation.

## Compromise and rotation

Treat suspected disclosure, unexpected signing, loss of exclusive custody, or
an unexplained fingerprint mismatch as a security incident:

1. Stop official releases, disable the Environment, remove or revoke its
   signing secret, preserve audit logs, and announce that catalog updates are
   frozen.
2. Determine the last known-good signed catalogs and release assets. Do not
   delete evidence or overwrite published signatures during investigation.
3. Generate a replacement Ed25519 key through a separately reviewed rotation
   procedure. Never silently reuse the ID `prime-unlimited-2026-1` for different
   public key bytes.
4. Assign a new key ID, commit only its public trust document, and review the
   fingerprint out of band.
5. Provision the new public trust root safely to both JC11S and JP24 before
   relying on catalogs signed only by the new key. The current device contract
   is single-root; rotation therefore requires an explicit staged rollout and
   rollback plan, not an in-place file replacement during a normal mod release.
6. Re-sign and publish only after both device paths, recovery behavior, branch
   controls, Environment secret, and offline backup have been verified.
7. Document the incident boundary, affected releases, revocations, and the
   retirement status of the old key.

Routine key rotation follows the same staged process. Rotation is a distinct
security change and must not be hidden inside an ordinary mod release.

## Invalid-signature troubleshooting

Fail closed. Do not bypass verification, regenerate a key, or edit a catalog to
make an error disappear.

1. Confirm the catalog, detached `.sig.json`, and trust JSON belong to the same
   target and release. Verify their downloaded SHA-256 values against the
   published inventories.
2. Run the existing verifier against the exact files:

   ```sh
   go run modloader/catalog/tools/sign_catalog.go \
     -verify \
     -catalog modloader/catalog/jc11s.json \
     -signature modloader/catalog/jc11s.sig.json \
     -public-key modloader/catalog/trust/prime-unlimited-2026-1.json
   ```

   Repeat with `jp24.json` and `jp24.sig.json` for JP24.
3. Confirm key ID `prime-unlimited-2026-1` and fingerprint
   `17bafd5349bc7338b09697ef346b3fd796b2af09aa7c54f82d0c68d95e6f1441`.
4. Check for changed whitespace or line endings: signatures cover exact catalog
   bytes. Do not reformat signed JSON.
5. Check for a mismatched target, partial asset upload, stale catalog base,
   changed branch tip, wrong tag/commit/run ID, failed attestation, or an
   incomplete Environment approval.
6. On a device, verify that
   `/data/prime-unlimited/trust/catalog-key.json` is the expected public trust
   document and is a safe root-owned regular file. Never copy a private key to
   a device.
7. If the Release was published but the catalog push failed, correct the
   branch/ruleset issue and rerun the exact official dispatch. If bytes differ,
   stop and investigate instead of overwriting assets.
8. Escalate unexplained key or fingerprint mismatches as a possible compromise.

Record diagnostic hashes, run IDs, commit SHAs, tag names, public fingerprints,
and verifier errors. Never record secret contents, private-key paths, or private
key material.

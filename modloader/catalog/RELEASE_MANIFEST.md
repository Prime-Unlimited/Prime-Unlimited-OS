# Release manifest and approval states

`tools/assemble_catalog.py` merges one approved target of a cross-device release
into that target's canonical catalog: `jc11s.json` or `jp24.json`. It never
signs and never reads a private key. Run it separately for each device with
`--target`; the matching `catalogId` is fixed by `release-targets.json`.

The release manifest is data produced from final ZIPs. It contains
`schemaVersion: 1`, an `expectedBaseCatalogSha256` map keyed by device, and one entry for
every target in `release-targets.json`. Each target records the final filename,
HTTPS URL, SHA-256, byte size, exact runtime identity, QtQuick version, catalog
record, and five independent booleans:

- `built`: the matrix build completed;
- `structurallyValidated`: package inventory and modes passed validation;
- `technicallyCompatible`: runtime model, firmware, architecture, ABI, Qt and
  required host capabilities are confirmed;
- `hardwareVerified`: the exact mod behavior was tested on that target;
- `officialCatalogApproved`: maintainers explicitly approved publication.

Only a target with all five values true can be merged. An unapproved target
must still be present so the workflow proves the complete 32-/64-bit matrix,
but it is omitted from the official catalog. Compatibility values must exactly
match `release-targets.json`; unknown, empty, or guessed values fail closed.

The assembler verifies each ZIP from a local artifact directory before using
its digest or size. It replaces only an existing record with the same stable
target-specific ID, preserves all unrelated mods and libraries, refuses to
silently un-revoke a record, sorts records deterministically, verifies the
expected per-device base digest, and publishes each candidate through an atomic
rename. A changed artifact or integration record at the same public version
must increase integer `revision`; new records start at revision 1, matching the
runtime update contract.

Historical signed catalogs and signatures are immutable. The canonical sources
remain the already deployed per-device catalogs and detached signatures.
Release automation must re-check each applicable base digest immediately before
signing and publication. The schema advertises `update`, but the strict runtime
mod struct does not accept that field; the assembler therefore rejects it.

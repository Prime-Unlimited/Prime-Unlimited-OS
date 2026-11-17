# Verified JC11S dependency/conflict preflight status

Verified on 2026-11-17 against a Denon DJ Prime 4+ (`JC11S`) running Engine
OS 5.3.0.

## Deployed artifacts

- Runtime binary: 6,619,320-byte static ARM EABI5 executable
  - SHA-256: `3b92e80b0218a2f7f2e93d373f6f7fbd215998bf9e938f0f8d5cc8ed8228296a`
- Pre-milestone backups: `/data/prime-unlimited/backups/resolver-v1/`

## Verification

The authenticated resolver processed the signed v2 device-test catalog and
returned the deterministic plan:

```json
{"id":"inspect-device-test","order":[{"type":"mod","id":"inspect-device-test","version":"44e96b9"}]}
```

The operation left the installed registry and activation manifest empty.
Runtime API and Engine remained active, Engine retained PID 698, and no
package was downloaded, installed, enabled or executed.

Local tests cover comparison intersections, caret, tilde and wildcard
constraints; transitive mod and library ordering; missing or incompatible
versions; cycles; direct and reverse conflicts; enabled-installed conflicts;
and authenticated API access.

## Restored device state

The signed v1 smoke catalog was restored and the temporary test script and
result were removed. The resolver-enabled runtime remains deployed. No Engine
restart or device reboot occurred.

The endpoint is deliberately preflight-only. Dependency-bearing packages
remain blocked from inspection and installation until a later milestone can
atomically install and activate the complete resolved set.

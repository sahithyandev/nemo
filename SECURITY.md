# Security policy

## Supported versions

nemo is early-stage software. Only the latest release gets security fixes.

## Reporting a vulnerability

Please do not open a public issue for security problems.

Report privately through GitHub: go to the
[Security tab](https://github.com/sahithyandev/nemo/security/advisories/new)
and choose "Report a vulnerability".

Include what you can of:

- the nemo version (`nemo version`) and your OS
- the filesystem type and a minimal image or steps that trigger the problem
- what you expected and what happened

You should get a reply within 7 days. Fixes are developed privately and
published with a GitHub security advisory once a release is out.

## Scope

In scope:

- memory safety or crash bugs when parsing untrusted disk images
- writes outside the intended image, file, or region
- bypasses of the chain-of-custody write logging
- problems in the release pipeline or published artifacts

Out of scope:

- the hiding techniques themselves being detectable by forensic tools. That
  is a known limitation of slack-space and named-stream methods, not a
  vulnerability.
- misuse of nemo against systems you do not own

## Verifying releases

Each release ships `checksums.txt` (SHA-256) and a CycloneDX SBOM
(`sbom.cdx.json`). Check your download with:

```
sha256sum --ignore-missing -c checksums.txt
```

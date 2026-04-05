# Security Policy

## Supported Versions

Only the latest release receives security fixes.

| Version | Supported |
|---|---|
| latest | ✓ |
| older  | ✗ |

## Reporting a Vulnerability

**Do not open a public GitHub issue for security vulnerabilities.**

Report security issues via [GitHub's private vulnerability reporting](https://github.com/commit0-dev/commit0/security/advisories/new).
You should receive a response within **72 hours**. If the issue is confirmed, a patch will be released as soon as possible, typically within 7 days for critical issues.

Please include:
- A description of the vulnerability and its impact
- Steps to reproduce or a proof-of-concept
- Any suggested mitigations (optional)

## Supply Chain Security

Every release artifact can be verified:

### Container image (cosign keyless)

```sh
cosign verify \
  --certificate-identity-regexp="https://github.com/commit0-dev/commit0/.github/workflows/release.yml" \
  --certificate-oidc-issuer="https://token.actions.githubusercontent.com" \
  ghcr.io/commit0-dev/commit0:<version>
```

### SBOM

SBOM attestations are attached to each image and can be retrieved with:

```sh
cosign download attestation ghcr.io/commit0-dev/commit0:<version> \
  | jq -r '.payload' | base64 -d | jq '.predicate'
```

### Build provenance

```sh
gh attestation verify \
  oci://ghcr.io/commit0-dev/commit0:<version> \
  --owner commit0-dev
```

## Vulnerability Scanning

- **Go dependencies**: scanned on every CI run with `govulncheck`
- **Container image**: scanned on every CI run with Trivy
- **CodeQL**: SAST analysis runs on every push to `main` and weekly
- **Dependency review**: runs on every pull request

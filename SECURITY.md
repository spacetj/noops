# Security Policy

## Supported versions

Security fixes are released as patch versions on the latest major release. When
new major versions are cut, the previous major receives security fixes for at
least 6 months.

## Reporting a vulnerability

If you believe you have found a security vulnerability in **noops**:

1. **Do not** open a public issue.
2. Email the maintainers at `security@noops.dev` with the details and a proof of
   concept if available.
3. The team will acknowledge receipt within 72 hours and coordinate on a fix and
   disclosure timeline.

If email is not an option, you may create a private security advisory via the
GitHub Security tab for the repository.

## Preferred practices

- Use a dedicated staging Notion workspace when reproducing issues.
- Sanitize tokens, database IDs, and page IDs from any logs or stack traces
  before sharing.
- Give the maintainers time to release a patched version before publicly
  discussing the vulnerability.

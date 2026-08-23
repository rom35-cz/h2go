# Security Policy

## Supported versions

Only the latest tagged release receives security fixes.

## Reporting a vulnerability

Please do **not** open a public issue for security problems.

Use GitHub's private vulnerability reporting:
<https://github.com/rom35-cz/h2go/security/advisories/new>

Include a description, the affected version/commit, and — if possible — a
minimal reproduction (DSN shape + SQL + driver calls). You can expect an
initial response within a few days; fixes land in the next release and are
credited unless you prefer otherwise.

## Scope notes

- The driver speaks H2 native TCP protocol 21 to servers the user points it
  at. Reports about H2 server-side behavior belong upstream at
  <https://github.com/h2database/h2database/security>.
- The wire protocol has no authentication confidentiality beyond TLS: run
  production traffic over `ssl://` connections or a trusted network.
- DSN parameters other than `USER`/`PASSWORD` are parsed but not applied
  (see README); do not rely on them for security posture.

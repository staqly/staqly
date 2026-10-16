# Security policy

staqly stores content your agents produce and credentials for chat
platforms, so we take security reports seriously.

## Reporting a vulnerability

Please **do not open a public issue**. Report privately via
[GitHub security advisories](https://github.com/staqly/staqly/security/advisories/new).
Include the affected version/commit, mode (local or cloud), steps to
reproduce and the impact you see. We aim to acknowledge reports within
3 business days and will keep you updated until a fix is released.

## Scope

In scope: the API (`apps/api`), dashboard (`apps/dashboard`), CLI, SDKs, the
Docker setup and documented defaults. Especially: authentication and scopes,
workspace isolation, local-mode access checks (Host / Origin /
X-Forwarded-Host), CSRF, SSRF guards for webhooks/watchers/notifications,
secret storage, media handling and decision links.

Out of scope: issues requiring an already-compromised host or Docker daemon,
and self-inflicted misconfiguration (e.g. publishing the API port to the
internet in local mode against the documentation).

The security model is described in [docs/architecture.md](docs/architecture.md)
and the documentation site (Self-hosting → Security model).

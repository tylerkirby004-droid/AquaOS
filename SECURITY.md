# Security policy

Do not disclose suspected vulnerabilities in a public issue. Use GitHub's
private vulnerability reporting for this repository. If that facility is
unavailable, contact the maintainer privately before sharing technical detail.

Only the latest released version receives security fixes before v1.0. Never
include aquarium credentials, configurations, addresses, backups, or logs with
secrets in a report. Include impact, affected revision, reproduction steps, and
a safe way to validate the fix.

High and critical findings block release. A temporary exception must be
documented in `docs/threat-model.md` with an owner, rationale, compensating
control, and expiry. AquaOS must not be exposed directly to the Internet.

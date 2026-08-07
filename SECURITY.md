# Security policy

## Reporting a vulnerability

Do not disclose suspected vulnerabilities in a public issue. Use GitHub's
private vulnerability reporting for this repository:

https://github.com/tylerkirby004-droid/AquaOS/security/advisories/new

Include affected versions, deployment assumptions, reproduction steps, impact,
and any suggested mitigation. Do not include real credentials, private network
details, or personal aquarium telemetry.

## Supported versions

AquaOS is pre-release software. No version is currently supported for
unsupervised control of a production aquarium. Security fixes are developed on
the current `main` branch until a supported-version policy is published for a
stable release.

## Safety and security boundary

AquaOS is not a substitute for independent physical safeguards, electrical
protection, local AquaOS safety policy, or responsible husbandry. Cloud services,
dashboards, databases, and optional AI services must not be required for safe
operation. AI credentials must never permit actuator commands.

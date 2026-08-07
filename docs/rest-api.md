# AquaOS REST API v1

The authoritative contract is [openapi-v1.yaml](openapi-v1.yaml). Process liveness remains available at `/health/live`; all `/api/v1` routes require authentication.

Configure `http.bearer_token_file` with an absolute, least-privilege-readable file containing the bearer token. AquaOS reads at most 4096 bytes during startup and never logs or returns the credential. A non-loopback listener is rejected without this setting.

Clients send `Authorization: Bearer <token>`. Mutations also carry `X-Correlation-ID` when joining an existing workflow. Equipment commands require `Idempotency-Key`, `expiresAt`, and should include `expectedRevision`. Alarm acknowledgements require a reason and expected canonical-state revision.

Candidate configuration is validated in memory. `/config/reload` asks the existing configuration manager to atomically reload the configured source; HTTP handlers never write raw files. Inventory mapping, discovery, backup/restore, and deployment-operation mutations remain reserved until their application services exist.

Errors use `application/problem+json`. Do not parse human-readable `detail`; use the stable `code`. List cursors are opaque and limits are bounded to 200 records.

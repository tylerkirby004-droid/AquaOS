# ADR-014: REST API v1 security and application boundary

- Status: Accepted
- Date: 2026-08-07

## Context

Home Assistant and the future Admin GUI need a stable interface to AquaOS Core. HTTP must not become an alternate hardware path or relocate safety policy into transport handlers.

## Decision

`/api/v1` is the only supported interactive mutation boundary. Handlers authenticate and authorize, enforce transport limits, translate DTOs, and invoke injected application services. The output service remains the sole equipment-command path. Configuration handlers may validate candidates or request the configuration manager's atomic reload; they never write configuration files.

Every protected route fails closed when no authenticator is configured. A file-supplied bearer credential is the initial minimal authentication provider. The interface permits replacement without changing handlers. Non-loopback listeners require an external credential file. Mutation rate and burst limits and request-size limits are external configuration.

Responses use correlation IDs and stable `application/problem+json` codes. Lists use bounded cursor pagination. Command idempotency and expected revisions are passed to the existing output service. Diagnostics are deliberately summaries and never serialize effective configuration.

## Consequences

The Admin GUI cannot bypass API authorization or safety policy. Credential rotation currently requires service restart. Stronger multi-user identity, sessions, CSRF policy, and recovery authentication belong to the Prompt 12 and 13 Admin GUI work.

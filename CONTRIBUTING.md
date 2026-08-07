# Contributing to AquaOS

AquaOS may eventually supervise equipment whose failure can harm livestock or
property. Reliability and review discipline take priority over feature speed.
Read [AGENTS.md](AGENTS.md) and the governing Development Bible before changing
code, contracts, safety policy, or deployment behavior.

## Workflow

1. Open or reference an issue with the problem, scope, failure modes, and safety
   impact.
2. Create a short-lived branch named `feat/<slug>`, `fix/<slug>`, `docs/<slug>`,
   `chore/<slug>`, `refactor/<slug>`, `test/<slug>`, or `security/<slug>`.
3. Keep commits focused, buildable, and imperative using Conventional Commit
   prefixes such as `feat:`, `fix:`, `docs:`, or `test:`.
4. Add deterministic tests for success and failure paths. Safety-relevant work
   must include boundary, stale-input, timeout, rejection, and recovery cases.
5. Update documentation, examples, compatibility notes, and an ADR when the
   architecture or a public contract changes.
6. Run the local verification commands before opening a pull request.

```sh
make fmt
make vet
make test
make lint
make build-all
```

## Pull request gate

A pull request must explain the change, safety impact, rollback path, tests, and
API/MQTT/config compatibility. Formatting, lint, vet, race tests, unit tests,
and supported builds must pass. Do not commit secrets, production credentials,
personal aquarium telemetry, generated local data, or large binary artifacts.

At least one reviewer is required. Safety-critical changes require a designated
safety reviewer or two reviewers. `main` must remain releasable and must never
be force-pushed.

## Scope discipline

Follow the milestone order in the Development Bible. Do not pull domain logic,
safety policy, MQTT contracts, hardware behavior, or optional AI work into an
earlier foundation change. AquaOS Core is authoritative; future hardware access
must use the sole output manager and explicit local adapter boundaries. Optional
services must never become part of aquarium survival.

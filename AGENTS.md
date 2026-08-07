# AquaOS Development Rules

Never sacrifice reliability for convenience.

Never introduce breaking architectural changes without documenting why.

Always write unit tests for new packages.

Never mix business logic into MQTT handlers.

Never mix HTTP handlers with equipment logic.

Keep interfaces small.

Prefer composition over inheritance.

Prefer explicit errors over panics.

All critical operations must be logged.

No hidden goroutines.

All goroutines must be cancellable through `context.Context`.

All configuration must be external.

No hardcoded IP addresses.

No hardcoded MQTT topics.

Document every public type and function.

Keep packages focused.

Business logic belongs in services, not handlers.

Write code that another engineer could maintain in five years.

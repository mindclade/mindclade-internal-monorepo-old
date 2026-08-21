# TypeScript authentication boundary

- **Status:** Implemented and unit tested; identity-provider integration remains unqualified.
- **Primary implementation ownership:** the language indicated by the second path segment

## Purpose

Cookie-backed login, logout, and current-session calls; a process-local session
store; and fail-closed React scope guards. The package never stores bearer
tokens and does not make UI visibility an authorization decision.

## Boundary

Reusable implementation belongs in this owning package. Deployable entry points,
provider construction, health/drain wiring, and deployment evidence belong under
`services/`. Cross-language data exchanged outside a process uses versioned
contracts under `protocols/` rather than language-private structures.

This package must not become a `common`, `shared`, `helpers`, or `utils` dumping
ground. It may depend only in the direction documented by
`docs/architecture/dependency-rules.md` and the accepted ADRs.

Servers remain authoritative for identity, scope, tenancy, expiry, revocation,
and audit. Consumers must clear session state on logout or authentication
failure and must not persist the in-memory state to browser storage.

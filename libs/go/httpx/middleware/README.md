# HTTP Middleware

`middleware` provides the canonical server-side HTTP middleware used by
Mindclade services. It is deliberately limited to transport concerns and
composes the transport-neutral contracts from `auth`, `faults`, `httpx`, and
`requestmeta`.

## Canonical stack

Use `middleware.Server` instead of assembling a different chain in every
service. The stack applies, from outermost to innermost:

1. request metadata extraction and response propagation;
2. access observation;
3. security response headers;
4. panic recovery and safe fault rendering;
5. an optional request-body limit;
6. optional authentication;
7. optional authorization;
8. explicitly supplied service middleware;
9. the application handler.

The ordering is intentional. Access observers see the final response status,
security headers remain present on recovered failures, and all downstream
handlers receive canonical request lineage.

## Boundaries

This package does not parse JWTs, define authorization policy, emit telemetry
through a global provider, execute retries, or own service lifecycle. Those
responsibilities remain in `auth`, service composition, `observability`,
`retry`, and `servicekit` respectively.

Middleware must return wire-safe `httpx.Problem` responses. It must never send
wrapped error causes, panic values, credentials, request bodies, or arbitrary
fault fields to clients.

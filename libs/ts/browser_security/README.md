# TypeScript browser security policy

- **Status:** Implemented and unit tested; connected ingress/CSP enforcement remains unqualified.
- **Primary implementation ownership:** the language indicated by the second path segment

## Purpose

Provides deterministic browser response headers, CSP construction, and public endpoint
validation for Mindclade browser applications. It rejects insecure non-local endpoints in
production and never treats browser policy as server-side authentication or authorization.

## Boundary

Applications supply the exact API and telemetry origins they need. This package owns only
browser policy mechanics; identity, tenant authorization, ingress TLS, and service policy
remain server and platform responsibilities.

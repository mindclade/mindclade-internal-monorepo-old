# auth

`auth` defines Mindclade's transport-neutral authentication and authorization
contracts.

Provider adapters validate opaque credentials and return immutable principals.
Services request explicit permissions over typed resources and enforce a
fail-closed authorization decision. The package includes no JWT parser, HTTP
middleware, RPC interceptor, database policy store, or provider-specific role
model.

Credential values are defensively copied and always redacted from `String`.
Principals snapshot issuer, subject, scope, permissions, attributes, and
lifetime so downstream audit records never depend on mutable identity state.

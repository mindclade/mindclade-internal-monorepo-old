# requestmeta

`requestmeta` is the authoritative, transport-neutral request correlation
contract for Mindclade Go services.

It owns canonical `request_*` UUIDv7 IDs, bounded external correlation and
causation tokens, logical operation names, context propagation, and generic
text-map propagation. It deliberately does not own authentication principals,
OpenTelemetry trace context, transport middleware, logging, or business data.

Inbound adapters should call `ExtractOrGenerate`; outbound adapters should call
`Inject`. Invalid propagated metadata fails closed instead of being silently
replaced.

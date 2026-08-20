# Python observability values

This package supplies provider-neutral, immutable values for structured logs, metric observations,
and trace propagation. It validates canonical names and finite numeric values, bounds labels and
attributes, redacts credential-like keys recursively, and strips user info, query strings, and
fragments from HTTP URLs before they become telemetry. W3C version-00 `traceparent` values round
trip through `TraceContext`.

Log events accept at most 64 fields and 2,048 message characters. Metrics accept at most 16 labels,
finite values, and non-negative counter observations. Redaction accepts at most 4,096 nodes,
16 levels, 128 fields per mapping, and 4,096 characters per string. Unknown objects are represented
by type only, and bytes by type and length, so their payloads do not leak through `repr`.

This package does not configure Python logging, register global metric instruments, install an
OpenTelemetry provider, export data, sample traces, or define platform-wide label vocabularies.
Applications adapt these values to their selected backend and remain responsible for cardinality
budgets across observations.

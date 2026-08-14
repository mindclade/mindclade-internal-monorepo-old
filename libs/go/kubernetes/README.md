# Kubernetes

Shared Mindclade adapters around Kubernetes API machinery and
`controller-runtime`.

The package owns error qualification, metadata and condition helpers,
controller middleware, finalizer and owner-reference mechanics, patch helpers,
event recording, watch coordination, and REST-client construction. It does not
implement a competing controller runtime, work queue, cache, or reconciliation
scheduler.

# `mindclade_worker_runtime`

Reusable state machine and fencing/resource envelope for ticketed stage workers.
It owns lifecycle mechanics, not ingestion/MSA/evaluation/model semantics.
Concrete worker executables remain in `services/workers`, `services/node_agent`,
`training`, and `serving`.

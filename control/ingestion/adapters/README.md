# Ingestion control adapters

This namespace contains thin control-plane adapters that translate durable
ingestion decisions into external workload or source-control operations. It may
adapt generated protocol clients, Kubernetes workload objects, or signed worker
commands. It must not implement scientific parsing, curation, or provider-
generic mechanisms already owned by `libs/go`.

Adapters are created by service composition roots and tested against the stable
`control/ingestion` interfaces.

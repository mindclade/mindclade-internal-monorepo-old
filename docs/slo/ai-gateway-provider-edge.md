# AI Gateway provider-edge SLO

The governed provider edge is a tier-0 policy and accounting boundary. Repository source defines
the indicators, but no production objective is approved until staging supplies representative
load, provider, PostgreSQL, Secure Web Proxy, and GKE measurements.

Required indicators are successful governed decisions, pre-dispatch rejection by semantic reason,
end-to-end and provider-added latency by fixed operation, in-flight concurrency, durable dispatch
rate, reconciliation-pending count and oldest age, measured versus max-charge terminalization,
control-plane dependency errors, provider transport/status errors, identity verification errors,
and Secure Web Proxy deny/TLS failures. Labels are restricted to fixed operation/result/state
taxonomies; workspace, subject, endpoint, model, request, reservation, trace, and payload values are
forbidden metric labels.

Candidate objectives and burn alerts must be derived from staging evidence and explicitly separate
policy denials, caller errors, provider errors, and Mindclade availability errors. Availability is
never restored by bypassing policy resolution, dispatch durability, reconciliation, identity
verification, TLS inspection, or the egress allowlist. A missing metric contract, stale policy
epoch, growing pending backlog, or lost audit/outbox lineage makes the serving path unavailable.

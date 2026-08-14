# Control-plane API integration example

This runnable example demonstrates the repository's required Go composition path:

- `servicekit.Assembly` for ordered startup, drain, and reverse-order shutdown;
- `httpx` for bounded JSON handling, structured faults, and server lifecycle;
- `requestmeta`, `identifiers`, and `audit` for request lineage and immutable audit records;
- a transactional-outbox-shaped write boundary using `coordination/outbox`;
- broker-neutral publication through `messaging`;
- a bounded event projector receiving through the in-memory conformance adapter.

The in-memory stores are intentional local/test adapters. Production services use the PostgreSQL outbox/audit stores and the configured messaging provider while keeping the same contracts.

```bash
go run ./examples/go/control_plane_api/cmd/control-plane-api
curl -sS -X POST http://127.0.0.1:8080/v1/runs \
  -H 'content-type: application/json' \
  -d '{"name":"novafold-evaluation"}'
```

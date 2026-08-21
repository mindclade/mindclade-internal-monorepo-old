# TypeScript SDK

- **Status:** Implemented; publishing and live-environment qualification remain separate release gates.
- **Owner:** developer platform
- **Consumers:** browser applications, Node.js automation, and typed API adapters

The SDK is the public consumer projection of `protocols/openapi/public.openapi.yaml`.
It also exports generated Protobuf-ES bindings for internal RPC/event consumers
that need the canonical binary contracts. It contains no server policy.

## Consumer contract

```ts
import { MindcladeClient } from "@mindclade/sdk-typescript";

const client = new MindcladeClient({
  baseUrl: "https://api.mindclade.ai",
  accessToken: () => identity.accessToken(),
  timeoutMs: 15_000,
});

for await (const run of client.runs.all({ pageSize: 50 })) {
  console.log(run.id, run.state);
}
```

`runs`, `datasets`, `models`, `artifacts`, and `evaluations` expose page and
async-iteration APIs. `inference.run` returns either a bounded synchronous
result or the durable run accepted by the service. `inference.stream` validates
bounded server-sent JSON events and propagates cancellation through
`AbortSignal`.

Authentication can be a fixed bearer token or an async token provider. Browser
BFF deployments can instead set `credentials: "include"`. GETs and operations
with explicit idempotency keys retry only 429/502/503/504 responses with bounded
backoff; other mutations are never retried implicitly. Failures are
`MindcladeError` values carrying stable code, HTTP status, request identity, and
structured problem details when supplied.

The request deadline covers credential resolution, all attempts, and retry
delays. JSON responses are streamed through an 8 MiB decoded-byte limit by
default (`maxResponseBytes` changes it), while inference events are capped at 1
MiB each. Pagination detects token cycles and stops after 10,000 pages unless a
lower `maxPages` is supplied to the pagination helper. Base URLs must be
absolute HTTP(S) URLs without embedded credentials, query, or fragment.

Generated Protobuf-ES modules are exported as stable subpaths for consumers
that need canonical binary contracts:

```ts
import { ModelDescriptorSchema } from
  "@mindclade/sdk-typescript/protobuf/mindclade/inference/v1/model_pb";
```

These modules reflect the canonical schema exactly, including deliberately
reserved `*Scaffold` messages. They do not promote scaffolded server domains or
provide policy decisions. Most public web consumers should use the typed HTTP
client; internal event/RPC adapters should use protobuf subpaths only when the
wire contract requires them.

## Generation and compatibility

Run `pnpm run generate` after changing a canonical schema and commit the derived
files. `pnpm run generate:check` regenerates in place and fails on drift. Never
edit `src/generated`. Public compatibility follows the versioned OpenAPI path;
protobuf field numbers and enum values follow `protocols/compatibility`.

The package is intentionally `private` until release provenance, registry
credentials, package signing, and a consumer canary are approved. Rollback is a
package-version revert; it does not roll back a server contract.

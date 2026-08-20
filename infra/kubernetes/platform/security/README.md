# Native admission security

This module adds Kubernetes `ValidatingAdmissionPolicy` controls to namespaces that explicitly
opt in through Mindclade labels. It complements Pod Security Admission and default-deny
NetworkPolicy; it does not replace image-signature enforcement, Workload Identity, or an
external secrets controller.

Protected pods must use non-placeholder digest-pinned images, a named ServiceAccount without
automatic tokens, restricted security contexts, and CPU/memory/ephemeral-storage bounds. Host
namespaces, hostPath, privileged containers, and ordinary ephemeral debug containers are denied.
Services are internal-only so public exposure remains centralized in Gateway API.

Namespaces labeled `mindclade.dev/workload-activation=blocked` additionally require zero
Deployment/StatefulSet replicas and suspended Jobs/JobSets. The policy and manifests are
separate defenses: removing the label alone does not activate a workload.

The Namespace policy also preserves the foundation-only `mindclade-system` admission,
blocked-activation, ownership, and Pod Security labels. A namespace update cannot remove or
weaken those controls while the foundation contract is installed.

Capacity namespaces add two fail-closed contracts. Jobs and JobSets must carry the exact
namespace-local Kueue queue and public `mindclade.dev/workload-class`. The Namespace object may
change from `blocked`/`kueue-enabled=false` to `active`/`kueue-enabled=true` only in one update
that removes its activation blocker and supplies non-zero SHA-256 references for the capacity
evidence, qualified release evidence, and complete activation bundle. These are immutable
content references, not embedded tickets or credentials; admission validates their form while
the promotion workflow validates the referenced records and signatures.

Before that Namespace update, the bundle creates an immutable ConfigMap named
`mindclade-capacity-contract-sha256-<64-lowercase-hex>`. Admission requires it to bind the exact
namespace queue/class and carry non-zero activation, capacity, release, and quota-manifest
digests. The name suffix is the SHA-256 of `.data` serialized as UTF-8, sorted-key, compact JSON
(`jq -S -c`). CEL can validate the object and reference formats, but it cannot look up another
ConfigMap or recompute SHA-256; the offline promotion validator must prove object existence,
suffix equality, and exact Namespace-to-ConfigMap digest equality. The base renders only
immutable `mindclade-capacity-contract-schema-v1` examples whose
capacity values are explicitly absent; there is no fabricated active capacity contract.

## Rollout and break glass

Render and schema-check first, then apply policies with their bindings in `Audit` plus `Deny` as
declared. A connected pre-production test must exercise valid and invalid objects and inspect
policy `status.typeChecking.expressionWarnings` before production promotion. Debugging requires
a separately governed namespace; do not weaken the protected namespace to attach an ephemeral
container.

If admission incorrectly blocks recovery, remove only the affected binding through the approved
break-glass procedure, retain the policy for evidence, repair the manifest, and immediately
restore the binding. Every such action is an incident and requires audit-log review.

# Native admission security

This module adds Kubernetes `ValidatingAdmissionPolicy` controls to namespaces that explicitly
opt in through Mindclade labels. It complements Pod Security Admission and default-deny
NetworkPolicy; it does not replace image-signature enforcement, Workload Identity, or an
external secrets controller.

Protected pods must use digest-pinned images, a named ServiceAccount without automatic tokens,
restricted security contexts, and CPU/memory/ephemeral-storage bounds. Host namespaces,
hostPath, privileged containers, and ordinary ephemeral debug containers are denied. Services
are internal-only so public exposure remains centralized in Gateway API.

Namespaces labeled `mindclade.dev/workload-activation=blocked` additionally require zero
Deployment/StatefulSet replicas and suspended Jobs/JobSets. The policy and manifests are
separate defenses: removing the label alone does not activate a workload.

## Rollout and break glass

Render and schema-check first, then apply policies with their bindings in `Audit` plus `Deny` as
declared. A connected pre-production test must exercise valid and invalid objects and inspect
policy `status.typeChecking.expressionWarnings` before production promotion. Debugging requires
a separately governed namespace; do not weaken the protected namespace to attach an ephemeral
container.

If admission incorrectly blocks recovery, remove only the affected binding through the approved
break-glass procedure, retain the policy for evidence, repair the manifest, and immediately
restore the binding. Every such action is an incident and requires audit-log review.

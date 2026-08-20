# Downstream Kueue chart patch

Kueue chart `0.19.1` emits `metadata.namespace` on the cluster-scoped
`MutatingWebhookConfiguration` and `ValidatingWebhookConfiguration`. The Kubernetes API ignores
or rejects namespace on cluster-scoped objects depending on the client/admission path, and the
field violates Mindclade's object-scope validation.

`remove-cluster-webhook-namespace.patch` removes only those two metadata fields. Namespaces on
webhook Service references remain unchanged. The vendored archive under `../charts/` is the
upstream archive with this patch applied; `versions.env` records the upstream OCI digest, the
upstream archive digest, and the downstream vendored archive digest separately.

The same deterministic repack adds `controller.enabled` and `crds.enabled` phase controls and
`Prune=false,Delete=false` to every CRD. GitOps renders CRDs with the controller disabled and
controllers with CRDs disabled; validation requires disjoint inventories and full-render union
parity.

For an upgrade, pull the exact OCI version into a temporary directory, verify its upstream digest
and archive digest, then run `platform/operator-system/repack_chart.py` with this patch. Update all
three locks and prove both phase parity and that cluster-scoped webhook objects render without
`metadata.namespace`. Do not run `helm dependency update` and commit its output. Remove the patch
when upstream no longer emits either field.

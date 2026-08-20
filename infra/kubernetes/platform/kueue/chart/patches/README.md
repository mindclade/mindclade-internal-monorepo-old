# Downstream Kueue chart patch

Kueue chart `0.19.1` emits `metadata.namespace` on the cluster-scoped
`MutatingWebhookConfiguration` and `ValidatingWebhookConfiguration`. The Kubernetes API ignores
or rejects namespace on cluster-scoped objects depending on the client/admission path, and the
field violates Mindclade's object-scope validation.

`remove-cluster-webhook-namespace.patch` removes only those two metadata fields. Namespaces on
webhook Service references remain unchanged. The vendored archive under `../charts/` is the
upstream archive with this patch applied; `versions.env` records the upstream OCI digest, the
upstream archive digest, and the downstream vendored archive digest separately.

For an upgrade, pull the exact OCI version into a temporary directory, verify its upstream
digest and archive digest, extract it, apply this patch with `patch -p1`, package it back into
`charts/`, and update all three locks. Do not run `helm dependency update` and commit its output
without reapplying the patch and proving that both cluster-scoped webhook objects render without
`metadata.namespace`. Remove the patch when upstream no longer emits either field.

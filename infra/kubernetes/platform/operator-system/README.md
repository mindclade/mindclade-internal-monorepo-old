# Transactional operator system

`foundation/resources.yaml` owns only the three operator namespaces and an ingress-and-egress
default-deny policy in each. It contains no workload, RBAC grant, Secret, or guessed network
exception. A controlled live overlay must add exact GKE control-plane, kubelet-probe, DNS, and
Managed Service for Prometheus flows before any controller Application is unpaused.

`repack_chart.py` deterministically adds two downstream lifecycle controls to verified upstream
operator charts:

- `controller.enabled` suppresses every non-CRD template for a CRD-only render;
- Kueue additionally receives `crds.enabled`, because its CRDs are Helm templates rather than
  files under Helm's `crds/` directory.

Every CRD receives `Prune=false,Delete=false`. The script rejects an upstream digest mismatch,
unsafe archive members, missing anchors, duplicate patching, and unsupported files. It does not
download dependencies. Repacking is a reviewed dependency-promotion action, never an ordinary
render step.

Promotion passes the upstream archive and the exact
`MINDCLADE_*_UPSTREAM_CHART_ARCHIVE_SHA256` from `infra/kubernetes/versions.env`; Kueue also passes
`infra/kubernetes/platform/kueue/chart/patches/remove-cluster-webhook-namespace.patch`. The script
verifies the
input before extraction, rejects unsafe members, then writes a sorted gzip/tar stream with zeroed
timestamps/ownership and normalized modes. Run it twice and byte-compare the outputs before
updating `MINDCLADE_*_VENDORED_CHART_ARCHIVE_SHA256`. The GitOps validator then proves CRD and
controller renders are disjoint and their union equals the full wrapper render.

The release identity is `cert-manager`, `jobset`, and `kueue`. Any cluster containing an older
`mindclade-*` Helm release must stop for an explicit adoption/migration plan; renaming an installed
release is not an in-place upgrade.

Operator namespaces use `mindclade.dev/admission: platform-operator` and
`mindclade.dev/workload-activation: platform-operator`. These deliberately do not select the
standard-workload policies used by `mindclade-system`: controllers require scoped Kubernetes API
credentials and upstream cert-manager explicitly automounts its ServiceAccount tokens. PSS stays
restricted. Before activation, the connected qualification must audit each operator RBAC grant,
token audience/rotation, API calls, and absence of a general-purpose workload identity. Controller
replicas remain gated by paused GitOps Applications and the operator-network activation review.

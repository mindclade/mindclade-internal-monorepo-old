# Environment-neutral Kubernetes packages

This directory owns reusable, environment-neutral Kustomize and Helm package source shipped
with monorepo releases. It does not own a development, staging, or production cluster view.

The authoritative `mindclade/gitops` repository selects immutable monorepo releases, supplies
environment overlays and image digests, renders desired state, and promotes it between
environments. Nothing in this directory is applied directly to a cluster.

## Package boundaries

| Path | Responsibility |
|---|---|
| `base/` | Environment-neutral namespace and safe-default primitives |
| `policies/` | Reusable fail-closed network and resource policy templates |
| `services/` | Deployable workload package templates without production image selection |
| `workloads/` | Durable Job/JobSet templates without environment activation |
| `platform/` | Versioned operator and platform package definitions |
| `planes/*/base/` | Plane-specific routing and service package templates |
| `tests/` | Offline render, schema, policy, and relational validation |

Production image references, cluster destinations, Argo CD Applications/AppProjects, and
environment capacity decisions are forbidden here.

## Validation

```bash
nix develop .#ci --command tools/dev/bazelw test \
  //infra/kubernetes:validate --test_output=errors
```

Validation is offline and must not require a kubeconfig or cluster credential.

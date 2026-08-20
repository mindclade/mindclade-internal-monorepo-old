# Kubernetes package release runbook

1. Change only environment-neutral package source in this repository.
2. Run `//infra/kubernetes:validate` in the pinned Nix toolchain.
3. Build and qualify the immutable monorepo release artifact.
4. Open a promotion pull request in `mindclade/gitops` that selects the immutable release and
   environment-specific image digests.
5. Render, review, and promote through development, staging, and production in `gitops`.

Do not run `kubectl apply` from this repository. Cluster rollout, health, and rollback evidence
belong to the authoritative GitOps change.

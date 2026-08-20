# Downstream JobSet chart phase control

The vendored archive is a deterministic repack of the exact upstream `jobset-0.12.0.tgz` bytes
locked in `infra/kubernetes/versions.env`. The promotion tool adds only:

- `controller.enabled`, wrapping every non-CRD template; and
- `argocd.argoproj.io/sync-options: Prune=false,Delete=false` on every CRD.

Render the CRD phase with `--include-crds --set jobset.controller.enabled=false`; render the
controller phase with `--skip-crds --set jobset.controller.enabled=true`. The GitOps validator
requires the phase inventories to be disjoint and their union to equal the full wrapper render.

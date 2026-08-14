# Go foundation qualification evidence

This directory captures the qualification evidence shipped with the scaffold.

- `offline-validation.txt` is the successful output of
  `tools/qualification/go/validate.sh offline`.
- `control-plane-api-race.txt` records the race-enabled API integration test.
- `api-profile.json`, `scheduler-profile.json`, and
  `ingestion-controller-profile.json` are executable capability manifests
  emitted by the standardized control-plane bootstrap.
- `bazel-package-inventory.txt` enumerates packages that declare Bazel build
  boundaries under `libs/go`.

The offline lane validates code that does not require downloading external
providers. `VALIDATION.md` documents the connected provider, Bazel, Nix, and
module-sum checks that remain required in CI.

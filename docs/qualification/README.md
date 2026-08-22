# Shipped qualification evidence

This directory contains local evidence generated for the scaffold archive.

- `go/` contains Go foundation validation output, role capability manifests,
  the control-plane API race test, and Bazel package inventory.
- `typescript-applications.md` records the TypeScript SDK/library/application
  source qualification and its explicit deployment preflight boundary.
- `scaffold-structure.json` records Python syntax, JSON, TOML, YAML, and local
  Markdown-link validation.
- `reference-training-platform.md` defines the bounded reference-platform
  qualification sequence and separates source gates from connected evidence.
- `terraform-module-v0.4.0.md` defines the protected, deterministic publication
  preflight for the planned Terraform module tag and keeps publication separate from
  plan/apply authority.

Root `VALIDATION.md` and `QUALIFICATION.md` define the exact claims and the
connected provider/Bazel/Nix/release evidence still required.

# Immutable NixOS developer workstation image

This package owns the environment-neutral source for Mindclade's `x86_64-linux` Compute Engine
developer-workstation image. `infrastructure-live` owns the Cloud Storage artifact bucket,
Compute Image resource, VM selection, IAM, networking, CMEK, and applied lifecycle.

The image contains Nix, the pinned build toolchain, Google guest integration, OpenSSH, and the
idle-shutdown unit. First boot performs no package installation and does not contact package
mirrors or `nixos.org`. The Terraform workstation module verifies the exact
`/etc/mindclade/image-contract.json` digest before mounting the persistent workspace disk.

Build the raw-disk archive and its separately hashable contract from a clean exact revision:

```sh
tools/dev/nixw build .#workstation-gce-image
tools/dev/nixw build .#workstation-gce-image-contract
```

After the paired shared-workflow `v5.0.0` release exists and its WIF/environment contract is
applied, a protected operator can dispatch `.github/workflows/nixos-image.yml` from `main`. The
reusable workflow builds from the exact platform SHA, rejects a dirty or mismatched contract,
and uploads one digest-named Cloud Storage object with a generation-zero precondition. The
resulting evidence supplies `source_uri`, `source_object_generation`, `source_sha256`, and
`image_contract_sha256` to `infrastructure-live`; it grants no Compute Image or rollout authority.

The release lane must build twice, require matching artifacts, generate SHA-256/SBOM/provenance,
and promote the same create-only object. A successful local build is source evidence only; a
workflow run creates only the retained GCS source object, never a Compute Image or workstation.

The boot disk owns `/nix`. The separate persistent disk owns workspaces and Bazel data, so an OS
or toolchain update publishes a replacement image rather than mutating or obscuring the running
generation. Rollback selects the previous immutable image self-link while preserving that disk.

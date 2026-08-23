# Developer workstation module

A single private developer workstation: an `x86_64-linux` machine reachable only through IAP TCP
forwarding, carrying a persistent data disk for `/nix` and the Bazel disk cache, and powering
itself off when idle.

It exists for two reasons. The first is operational — a long build or an agent session should
survive a dropped local network link, which it does because `tmux` runs on the instance and the
SSH tunnel is disposable. The second is structural: `remote-execution-base` is gated
`optionalAttrs pkgs.stdenv.hostPlatform.isLinux`, so an `aarch64-darwin` laptop cannot build the
Nix package the CI runner image extends. This module provides the Linux host that can.

## Access is IAP TCP forwarding, not IAP for Web

`iap_access` is a different surface. It binds `google_iap_web_backend_service_iam_member` with
`roles/iap.httpsResourceAccessor`, which governs a load-balancer backend behind a web consent
screen. Tunnelling to a VM needs `roles/iap.tunnelResourceAccessor` on
`google_iap_tunnel_instance_iam_member`, and that module cannot express it — it hard-fails on an
empty `backend_services` map and admits only bare group principals. Extending it would register as
a breaking interface change on a security-critical module for a reason unrelated to its purpose,
so this module implements tunnelling itself.

Three bindings are required, and the third is the one that gets forgotten:

| Binding | Role |
|---|---|
| `google_iap_tunnel_instance_iam_member` | `roles/iap.tunnelResourceAccessor` |
| `google_compute_instance_iam_member` | `roles/compute.osLogin` (or `osAdminLogin`) |
| `google_service_account_iam_member` | `roles/iam.serviceAccountUser` |

Without the third, OS Login satisfies IAM and then fails at connect. That presents as a broken
tunnel and sends the operator to debug the wrong layer.

`35.235.240.0/20` is a `local`, never an input. A configurable source range is how a rule that
exists to admit only IAP eventually admits `0.0.0.0/0`.

## What survives a stop

The data disk is a separate `google_compute_disk`, not an inline boot disk, and
`attached_disk` carries no `auto_delete` — so it survives both a stop and a destroy/recreate of the
instance. The startup script formats it **only** when `blkid` reports no filesystem; that single
condition is the difference between "survives a stop" and "silently ate the Nix store".

`/nix` is a bind mount from the data disk, ordered ahead of `nix-daemon.service`. The reference
`/nix/store` measures 46 GB, which is why the disk defaults to 500 GB rather than something that
looks generous until Bazel and `cargo` arrive.

Local SSD, when requested, holds the Bazel disk cache and nothing else. It is ephemeral; `/nix` on
scratch means a full closure rebuild after every stop, and the startup script refuses to continue
if `/nix` resolves to an NVMe device.

## Idle shutdown

A systemd timer polls a bounded counter. Idle means no `loginctl` sessions, a one-minute load
average under the threshold, and no process matching a bounded build pattern. The guest powers
*itself* off, which requires no IAM at all — a `gcloud compute instances stop` loop would need
`roles/compute.instanceAdmin.v1` on the instance's own identity, which is precisely the authority
this module refuses to hold.

`scheduling.automatic_restart` covers Compute-Engine-initiated terminations only, so it does not
undo a guest-initiated poweroff.

The optional daily schedule stops the instance and deliberately never starts it. A workstation that
starts itself bills for a machine nobody asked for.

## What this identity must never hold

The default project role set is the observability floor — `logging.logWriter` and
`monitoring.metricWriter` — and nothing else. `extra_project_roles` refuses basic roles, admin
roles, and an explicit deny list covering KMS signing, Binary Authorization attestation, container
analysis, Artifact Registry writes, and service-account token or key minting.

The workstation reads the Nix cache and reads/writes the Bazel cache. Those grants are *emitted*
as `required_cache_grants` rather than created here: the buckets belong to `nix_binary_cache` and
`bazel_remote_cache`, which already expose member inputs. Two Terraform states each believing they
own the same binding is how removing one revokes access the other still claims.

## The Nix cache is not a substituter

The startup script installs Nix but does **not** point it at the Nix cache bucket.
`nix_binary_cache` exports `substituter_uri = null` and `client_activation_contract.enabled =
false` with reason `raw-private-gcs-is-not-a-nix-substituter`; raw private GCS does not speak the
authenticated Nix cache protocol. The `objectViewer` grant is for tooling that reads objects.
Until a reviewed substituter service exists, this machine builds its closure locally.

This module also exports no NixOS, nix-darwin, or Home Manager configuration, matching
`tools/build/nix/README.md`: the repository owns toolchains, not workstation or server lifecycle.

## Machine types

`x86_64` only. Arm is refused in a validation whose error message names the reason: the `.#gpu`
shell is `x86_64-linux` only and no aarch64 CUDA target exists, so an Arm workstation could not
enter the shell it exists to run.

`c3d-*` is excluded despite being a reasonable technical fit — the organization holds no C3D quota.
The default is `c2d-standard-16`; `n2-standard-16` is the fallback. `t2d-*` is permitted but has no
regional quota headroom and does not support Local SSD.

## Confidential Compute

Not exposed. Enabling it forces `on_host_maintenance = "TERMINATE"`, so a host maintenance event
destroys the VM and the `tmux` session — directly against the reason this module exists. Adding it
later should be a reviewed decision that states that trade explicitly.

## Prerequisites this module does not create

- Cloud NAT and Private Google Access. The instance has no external address, so package and
  substituter egress depends on both.
- `roles/cloudkms.cryptoKeyEncrypterDecrypter` for the Compute Engine service agent on the CMEK.
- Cache bucket IAM, via `required_cache_grants`.
- The firewall rule, if `create_iap_ssh_firewall_rule = false`; `required_firewall_rule` still
  publishes the exact contract.

## Qualification

Mock tests prove configuration contracts and input rejection only. They do not prove tunnel
reachability, disk persistence across a real stop, idle-timer behaviour under a detached build,
NAT egress, or CMEK rotation safety. `qualification_requirements` enumerates what connected
evidence must cover.

<!-- BEGIN_TF_DOCS -->
## Requirements

| Name | Version |
| ---- | ------- |
| <a name="requirement_terraform"></a> [terraform](#requirement\_terraform) | >= 1.9.0, < 2.0.0 |
| <a name="requirement_google"></a> [google](#requirement\_google) | >= 7.41.0, < 8.0.0 |

## Providers

| Name | Version |
| ---- | ------- |
| <a name="provider_google"></a> [google](#provider\_google) | >= 7.41.0, < 8.0.0 |

## Inputs

| Name | Description | Type | Default | Required |
| ---- | ----------- | ---- | ------- | :------: |
| <a name="input_allow_stopping_for_update"></a> [allow\_stopping\_for\_update](#input\_allow\_stopping\_for\_update) | Permit Terraform to stop the instance when an update requires it | `bool` | `true` | no |
| <a name="input_bazel_cache_bucket_name"></a> [bazel\_cache\_bucket\_name](#input\_bazel\_cache\_bucket\_name) | Bazel remote cache bucket the workstation identity may read and write | `string` | n/a | yes |
| <a name="input_boot_disk_size_gb"></a> [boot\_disk\_size\_gb](#input\_boot\_disk\_size\_gb) | Boot disk size; the Nix store and Bazel cache live on the data disk, not here | `number` | `200` | no |
| <a name="input_create_iap_ssh_firewall_rule"></a> [create\_iap\_ssh\_firewall\_rule](#input\_create\_iap\_ssh\_firewall\_rule) | Create the IAP ingress rule here; set false when firewall rules are centralized | `bool` | `true` | no |
| <a name="input_daily_stop_schedule"></a> [daily\_stop\_schedule](#input\_daily\_stop\_schedule) | Optional cron stop schedule; there is deliberately no start schedule | `string` | `"0 3 * * *"` | no |
| <a name="input_data_classification"></a> [data\_classification](#input\_data\_classification) | Governance classification; public is forbidden | `string` | `"internal"` | no |
| <a name="input_data_disk_size_gb"></a> [data\_disk\_size\_gb](#input\_data\_disk\_size\_gb) | Persistent data disk carrying /nix and the Bazel disk cache | `number` | `500` | no |
| <a name="input_deletion_protection"></a> [deletion\_protection](#input\_deletion\_protection) | Guard against accidental instance deletion | `bool` | `true` | no |
| <a name="input_disk_type"></a> [disk\_type](#input\_disk\_type) | Disk type for both the boot and data disks | `string` | `"pd-balanced"` | no |
| <a name="input_environment"></a> [environment](#input\_environment) | Environment governance label | `string` | n/a | yes |
| <a name="input_extra_project_roles"></a> [extra\_project\_roles](#input\_extra\_project\_roles) | Additional project roles for the workstation identity; signing and release authority is refused | `set(string)` | `[]` | no |
| <a name="input_idle_check_interval_seconds"></a> [idle\_check\_interval\_seconds](#input\_idle\_check\_interval\_seconds) | Bounded polling interval for the idle check timer | `number` | `300` | no |
| <a name="input_idle_load_threshold"></a> [idle\_load\_threshold](#input\_idle\_load\_threshold) | One-minute load average below which the workstation counts as idle | `number` | `0.5` | no |
| <a name="input_idle_shutdown_minutes"></a> [idle\_shutdown\_minutes](#input\_idle\_shutdown\_minutes) | Bounded idle period after which the guest powers itself off | `number` | `60` | no |
| <a name="input_image"></a> [image](#input\_image) | Boot image as project/family or a full image self-link | `string` | `"debian-cloud/debian-12"` | no |
| <a name="input_kms_key_name"></a> [kms\_key\_name](#input\_kms\_key\_name) | Required CMEK protecting both the boot disk and the persistent data disk | `string` | n/a | yes |
| <a name="input_labels"></a> [labels](#input\_labels) | Additional resource labels merged under the module's baseline labels | `map(string)` | `{}` | no |
| <a name="input_local_ssd_count"></a> [local\_ssd\_count](#input\_local\_ssd\_count) | Ephemeral NVMe scratch disks for the Bazel cache only; never for /nix | `number` | `0` | no |
| <a name="input_machine_type"></a> [machine\_type](#input\_machine\_type) | x86\_64 machine type; Arm is forbidden by the repository's toolchain contract | `string` | `"c2d-standard-16"` | no |
| <a name="input_metadata"></a> [metadata](#input\_metadata) | Additional instance metadata; module-owned keys are refused | `map(string)` | `{}` | no |
| <a name="input_name"></a> [name](#input\_name) | Workstation instance name; derived resources append a suffix | `string` | n/a | yes |
| <a name="input_network"></a> [network](#input\_network) | Network owning the IAP ingress rule; required when this module creates that rule | `string` | `null` | no |
| <a name="input_network_tag"></a> [network\_tag](#input\_network\_tag) | Network tag binding the IAP ingress rule to this instance | `string` | `null` | no |
| <a name="input_nix_cache_bucket_name"></a> [nix\_cache\_bucket\_name](#input\_nix\_cache\_bucket\_name) | Nix binary cache bucket the workstation identity may read | `string` | n/a | yes |
| <a name="input_operator_principals"></a> [operator\_principals](#input\_operator\_principals) | Human principals permitted to open an IAP tunnel and log in | `set(string)` | n/a | yes |
| <a name="input_os_login_role"></a> [os\_login\_role](#input\_os\_login\_role) | OS Login role granted to operators; osAdminLogin grants sudo | `string` | `"roles/compute.osLogin"` | no |
| <a name="input_owner"></a> [owner](#input\_owner) | Accountable team governance label | `string` | n/a | yes |
| <a name="input_project_id"></a> [project\_id](#input\_project\_id) | Project that owns the workstation instance, disk, and identity | `string` | n/a | yes |
| <a name="input_region"></a> [region](#input\_region) | Region holding the persistent data disk's CMEK and the instance schedule | `string` | n/a | yes |
| <a name="input_schedule_timezone"></a> [schedule\_timezone](#input\_schedule\_timezone) | IANA time zone for the stop schedule | `string` | `"Etc/UTC"` | no |
| <a name="input_service_account_id"></a> [service\_account\_id](#input\_service\_account\_id) | Account id for the workstation's dedicated keyless identity | `string` | n/a | yes |
| <a name="input_subnetwork"></a> [subnetwork](#input\_subnetwork) | Fully qualified subnetwork hosting the workstation's only interface | `string` | n/a | yes |
| <a name="input_zone"></a> [zone](#input\_zone) | Zone for the instance and its zonal persistent disk | `string` | n/a | yes |

## Outputs

| Name | Description |
| ---- | ----------- |
| <a name="output_builder_contract"></a> [builder\_contract](#output\_builder\_contract) | What this workstation can and cannot build for the remote-execution base |
| <a name="output_cache_access_contract"></a> [cache\_access\_contract](#output\_cache\_access\_contract) | Cache authority this workstation is designed to hold, and what it must never hold |
| <a name="output_data_disk"></a> [data\_disk](#output\_data\_disk) | The persistent disk carrying /nix and the Bazel disk cache |
| <a name="output_iap_access_contract"></a> [iap\_access\_contract](#output\_iap\_access\_contract) | The IAP TCP forwarding contract this module implements |
| <a name="output_instance"></a> [instance](#output\_instance) | Identifying attributes of the workstation instance |
| <a name="output_qualification_requirements"></a> [qualification\_requirements](#output\_qualification\_requirements) | Connected evidence this module's contracts require but cannot prove |
| <a name="output_required_apis"></a> [required\_apis](#output\_required\_apis) | Services that must be enabled on the project |
| <a name="output_required_cache_grants"></a> [required\_cache\_grants](#output\_required\_cache\_grants) | Grants the cache-owning modules must apply for this identity |
| <a name="output_required_firewall_rule"></a> [required\_firewall\_rule](#output\_required\_firewall\_rule) | The IAP ingress rule this workstation requires |
| <a name="output_required_grants"></a> [required\_grants](#output\_required\_grants) | Grants outside this module's authority that must exist before apply |
| <a name="output_required_network_prerequisites"></a> [required\_network\_prerequisites](#output\_required\_network\_prerequisites) | Network conditions this private instance depends on |
| <a name="output_service_account"></a> [service\_account](#output\_service\_account) | The workstation's dedicated keyless identity |
| <a name="output_shutdown_policy"></a> [shutdown\_policy](#output\_shutdown\_policy) | How this workstation stops, and what survives when it does |
| <a name="output_ssh_command"></a> [ssh\_command](#output\_ssh\_command) | The only supported access path |
<!-- END_TF_DOCS -->

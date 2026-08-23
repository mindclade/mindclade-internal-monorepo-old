# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

output "instance" {
  description = "Identifying attributes of the workstation instance"
  value = {
    id          = google_compute_instance.workstation.id
    name        = google_compute_instance.workstation.name
    self_link   = google_compute_instance.workstation.self_link
    instance_id = google_compute_instance.workstation.instance_id
    zone        = google_compute_instance.workstation.zone
    internal_ip = google_compute_instance.workstation.network_interface[0].network_ip
  }
}

output "service_account" {
  description = "The workstation's dedicated keyless identity"
  value = {
    id        = google_service_account.workstation.id
    email     = google_service_account.workstation.email
    name      = google_service_account.workstation.name
    unique_id = google_service_account.workstation.unique_id
  }
}

output "data_disk" {
  description = "The persistent disk carrying /nix and the Bazel disk cache"
  value = {
    id          = google_compute_disk.data.id
    name        = google_compute_disk.data.name
    self_link   = google_compute_disk.data.self_link
    size_gb     = google_compute_disk.data.size
    type        = google_compute_disk.data.type
    device_name = local.data_device_name
  }
}

output "iap_access_contract" {
  description = "The IAP TCP forwarding contract this module implements"
  value = {
    role          = "roles/iap.tunnelResourceAccessor"
    os_login_role = var.os_login_role
    members       = sort(tolist(var.operator_principals))
    source_ranges = local.iap_tcp_forwarding_source_ranges
    port          = 22
    tunnel_scope  = "instance"
  }
}

# Emitted whether or not this module created the rule, so an estate that centralizes firewall
# ownership can set create_iap_ssh_firewall_rule = false and still receive the exact contract.
output "required_firewall_rule" {
  description = "The IAP ingress rule this workstation requires"
  value = {
    created       = var.create_iap_ssh_firewall_rule
    direction     = "INGRESS"
    priority      = 1000
    source_ranges = local.iap_tcp_forwarding_source_ranges
    target_tags   = [local.network_tag]
    protocol      = "tcp"
    ports         = ["22"]
  }
}

output "cache_access_contract" {
  description = "Cache authority this workstation is designed to hold, and what it must never hold"
  value = {
    nix_cache = {
      bucket = var.nix_cache_bucket_name
      role   = "roles/storage.objectViewer"
    }
    bazel_cache = {
      bucket       = var.bazel_cache_bucket_name
      roles        = ["roles/storage.objectViewer", "roles/storage.objectCreator"]
      object_admin = false
    }
    signing_authority     = false
    publication_authority = false
    attestation_authority = false
  }
}

# The buckets are owned by nix_binary_cache and bazel_remote_cache, which already expose member
# inputs for exactly this. Emitting the required grant rather than creating it keeps one owner per
# bucket IAM binding: two states each believing they own a binding means removing one revokes
# access the other still claims.
output "required_cache_grants" {
  description = "Grants the cache-owning modules must apply for this identity"
  value = {
    nix_binary_cache_reader_member = "serviceAccount:${google_service_account.workstation.email}"
    bazel_remote_cache_rw_member   = "serviceAccount:${google_service_account.workstation.email}"
  }
}

output "required_apis" {
  description = "Services that must be enabled on the project"
  value = [
    "cloudkms.googleapis.com",
    "compute.googleapis.com",
    "iam.googleapis.com",
    "iap.googleapis.com",
    "oslogin.googleapis.com",
    "storage.googleapis.com",
  ]
}

output "required_grants" {
  description = "Grants outside this module's authority that must exist before apply"
  value = [
    "Compute Engine service agent requires roles/cloudkms.cryptoKeyEncrypterDecrypter on ${var.kms_key_name}",
    "Instance schedule requires the Compute Engine service agent to hold roles/compute.instanceAdmin.v1 when daily_stop_schedule is set",
  ]
}

output "required_network_prerequisites" {
  description = "Network conditions this private instance depends on"
  value = {
    private_google_access = true
    cloud_nat_required    = true
    external_ip           = false
    note                  = "The instance has no external address. Package and Nix substituter egress requires Cloud NAT, and Private Google Access is required for Google APIs."
  }
}

output "shutdown_policy" {
  description = "How this workstation stops, and what survives when it does"
  value = {
    mechanism                   = "guest-initiated-poweroff"
    idle_shutdown_minutes       = var.idle_shutdown_minutes
    idle_check_interval_seconds = var.idle_check_interval_seconds
    idle_load_threshold         = var.idle_load_threshold
    idle_cycles_before_poweroff = local.idle_cycles_before_poweroff
    daily_stop_schedule         = var.daily_stop_schedule
    vm_start_schedule           = null
    persistent_paths            = ["/nix", local.data_mount_point]
    ephemeral_paths             = var.local_ssd_count > 0 ? [local.local_ssd_mount] : []
  }
}

output "builder_contract" {
  description = "What this workstation can and cannot build for the remote-execution base"
  value = {
    system                = "x86_64-linux"
    nix_package           = "remote-execution-base"
    flake_attribute       = ".#packages.x86_64-linux.remote-execution-base"
    covers                = ["x86_64-linux"]
    does_not_cover        = ["aarch64-linux"]
    attestation_authority = false
    note                  = "remote-execution-base is gated to Linux hosts, so an aarch64-darwin laptop cannot build it. This workstation covers x86_64-linux only; an aarch64-linux builder remains required separately."
  }
}

output "ssh_command" {
  description = "The only supported access path"
  value       = "gcloud compute ssh ${google_compute_instance.workstation.name} --project=${var.project_id} --zone=${var.zone} --tunnel-through-iap"
}

output "qualification_requirements" {
  description = "Connected evidence this module's contracts require but cannot prove"
  value = [
    "An approved operator principal opens an IAP tunnel and an unapproved principal is denied.",
    "The data disk survives a stop/start cycle with /nix intact and no reformat.",
    "The idle timer powers the instance off when idle and does NOT fire during a detached tmux build.",
    "nix build .#remote-execution-base succeeds and reproduces the expected x86_64-linux digest.",
    "Cloud NAT egress reaches every required substituter and package source.",
    "CMEK rotation does not orphan the persistent data disk.",
    "Observed cost per idle day matches the idle-shutdown design.",
  ]
}

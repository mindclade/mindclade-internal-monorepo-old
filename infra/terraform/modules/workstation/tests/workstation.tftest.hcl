# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

mock_provider "google" {}

override_resource {
  target          = google_service_account.workstation
  override_during = plan
  values = {
    email = "sa-workstation@mindclade-development.iam.gserviceaccount.com"
    name  = "projects/mindclade-development/serviceAccounts/sa-workstation@mindclade-development.iam.gserviceaccount.com"
  }
}

variables {
  project_id              = "mindclade-development"
  name                    = "devbox"
  region                  = "us-central1"
  zone                    = "us-central1-a"
  network                 = "projects/mindclade-development/global/networks/dev-vpc"
  subnetwork              = "projects/mindclade-development/regions/us-central1/subnetworks/dev-workstations"
  kms_key_name            = "projects/mc-b-seed-fb7649/locations/us-central1/keyRings/dev/cryptoKeys/workstation"
  service_account_id      = "sa-workstation"
  operator_principals     = ["user:robpearc@mindclade.com"]
  nix_cache_bucket_name   = "mc-nix-binary-cache"
  bazel_cache_bucket_name = "mc-bazel-remote-cache"
  environment             = "development"
  owner                   = "platform"
}

run "private_iap_only_contract" {
  command = plan

  assert {
    condition = (
      length(google_compute_instance.workstation.network_interface[0].access_config) == 0 &&
      google_compute_instance.workstation.can_ip_forward == false &&
      google_compute_instance.workstation.metadata["enable-oslogin"] == "TRUE" &&
      google_compute_instance.workstation.metadata["block-project-ssh-keys"] == "TRUE" &&
      google_compute_instance.workstation.metadata["serial-port-enable"] == "FALSE"
    )
    error_message = "The workstation must be reachable only through IAP: no external address, no IP forwarding, OS Login enforced, project SSH keys blocked, and serial access denied."
  }

  assert {
    condition = (
      google_compute_instance.workstation.shielded_instance_config[0].enable_secure_boot == true &&
      google_compute_instance.workstation.shielded_instance_config[0].enable_vtpm == true &&
      google_compute_instance.workstation.shielded_instance_config[0].enable_integrity_monitoring == true &&
      google_compute_instance.workstation.advanced_machine_features[0].enable_nested_virtualization == false
    )
    error_message = "Shielded VM must be fully enabled and nested virtualization refused."
  }

  assert {
    condition     = contains(google_compute_firewall.iap_ssh[0].source_ranges, "35.235.240.0/20") && length(google_compute_firewall.iap_ssh[0].source_ranges) == 1
    error_message = "The ingress rule must admit only Google's published IAP TCP forwarding range."
  }

  assert {
    condition     = google_compute_firewall.iap_ssh[0].direction == "INGRESS" && google_compute_firewall.iap_ssh[0].priority == 1000
    error_message = "The IAP rule must be an ingress rule at the module's declared priority."
  }

  assert {
    condition = alltrue([
      for a in google_compute_firewall.iap_ssh[0].allow :
      a.protocol == "tcp" && contains(a.ports, "22") && length(a.ports) == 1
    ])
    error_message = "The IAP rule must open TCP port 22 and nothing else."
  }

  assert {
    condition = alltrue([
      for m in values(google_iap_tunnel_instance_iam_member.operator) :
      m.role == "roles/iap.tunnelResourceAccessor"
    ])
    error_message = "Tunnel access must be granted through roles/iap.tunnelResourceAccessor, not the IAP-for-Web role."
  }

  assert {
    condition = alltrue([
      for m in values(google_service_account_iam_member.operator_act_as) :
      m.role == "roles/iam.serviceAccountUser"
    ])
    error_message = "Operators require serviceAccountUser on the instance identity or OS Login passes IAM and then fails at connect."
  }
}

run "persistent_data_disk_contract" {
  command = plan

  assert {
    condition = (
      google_compute_disk.data.size == 500 &&
      google_compute_disk.data.type == "pd-balanced" &&
      google_compute_disk.data.disk_encryption_key[0].kms_key_self_link == var.kms_key_name &&
      google_compute_instance.workstation.boot_disk[0].kms_key_self_link == var.kms_key_name
    )
    error_message = "Both disks must be CMEK encrypted and the data disk must be a separate persistent resource."
  }

  assert {
    condition = (
      google_compute_instance.workstation.attached_disk[0].device_name == "workstation-data" &&
      contains(output.shutdown_policy.persistent_paths, "/nix")
    )
    error_message = "The Nix store must live on the persistent data disk so it survives an instance stop."
  }
}

run "least_privilege_identity_contract" {
  command = plan

  assert {
    condition = toset([for m in values(google_project_iam_member.workstation) : m.role]) == toset([
      "roles/logging.logWriter",
      "roles/monitoring.metricWriter",
    ])
    error_message = "The default project role set must be the observability floor and nothing else."
  }

  assert {
    condition = (
      output.cache_access_contract.bazel_cache.object_admin == false &&
      output.cache_access_contract.nix_cache.role == "roles/storage.objectViewer" &&
      output.cache_access_contract.signing_authority == false &&
      output.cache_access_contract.attestation_authority == false &&
      output.builder_contract.attestation_authority == false
    )
    error_message = "A developer workstation must never hold signing, publication, or attestation authority."
  }

  assert {
    condition     = google_service_account.workstation.deletion_policy == "PREVENT"
    error_message = "The workstation identity must be deletion protected."
  }
}

run "x86_builder_contract" {
  command = plan

  assert {
    condition = (
      output.builder_contract.system == "x86_64-linux" &&
      output.builder_contract.nix_package == "remote-execution-base" &&
      contains(output.builder_contract.does_not_cover, "aarch64-linux")
    )
    error_message = "The builder contract must claim x86_64-linux only and state plainly that aarch64-linux remains uncovered."
  }
}

run "bounded_shutdown_contract" {
  command = plan

  assert {
    condition = (
      output.shutdown_policy.vm_start_schedule == null &&
      length(google_compute_resource_policy.daily_stop) == 1 &&
      length(google_compute_resource_policy.daily_stop[0].instance_schedule_policy[0].vm_stop_schedule) == 1
    )
    error_message = "The schedule must stop the workstation and never start it; a self-starting workstation bills for a machine nobody asked for."
  }

  assert {
    condition     = output.shutdown_policy.idle_cycles_before_poweroff == 12
    error_message = "The idle counter must be bounded and derived from the configured interval and threshold."
  }
}

run "centralized_firewall_opt_out" {
  command = plan

  variables {
    create_iap_ssh_firewall_rule = false
  }

  assert {
    condition = (
      length(google_compute_firewall.iap_ssh) == 0 &&
      output.required_firewall_rule.created == false &&
      output.required_firewall_rule.source_ranges == ["35.235.240.0/20"]
    )
    error_message = "Opting out of rule creation must still publish the exact required rule contract."
  }
}

run "reject_arm_machine_type" {
  command = plan

  variables {
    machine_type = "c4a-standard-16"
  }

  expect_failures = [var.machine_type]
}

run "reject_c3d_without_quota" {
  command = plan

  variables {
    machine_type = "c3d-standard-16"
  }

  expect_failures = [var.machine_type]
}

run "reject_zone_outside_region" {
  command = plan

  variables {
    zone = "us-east4-b"
  }

  expect_failures = [google_compute_instance.workstation]
}

run "reject_public_operator_principal" {
  command = plan

  variables {
    operator_principals = ["allAuthenticatedUsers"]
  }

  expect_failures = [var.operator_principals]
}

run "reject_service_account_operator_principal" {
  command = plan

  variables {
    operator_principals = ["serviceAccount:ci@mindclade-development.iam.gserviceaccount.com"]
  }

  expect_failures = [var.operator_principals]
}

run "reject_domain_operator_principal" {
  command = plan

  variables {
    operator_principals = ["domain:mindclade.com"]
  }

  expect_failures = [var.operator_principals]
}

run "reject_empty_operator_principals" {
  command = plan

  variables {
    operator_principals = []
  }

  expect_failures = [var.operator_principals]
}

run "reject_signing_authority_role" {
  command = plan

  variables {
    extra_project_roles = ["roles/cloudkms.signerVerifier"]
  }

  expect_failures = [var.extra_project_roles]
}

run "reject_attestation_authority_role" {
  command = plan

  variables {
    extra_project_roles = ["roles/binaryauthorization.attestorsVerifier"]
  }

  expect_failures = [var.extra_project_roles]
}

run "reject_admin_role" {
  command = plan

  variables {
    extra_project_roles = ["roles/compute.instanceAdmin.v1"]
  }

  expect_failures = [var.extra_project_roles]
}

run "reject_shared_cache_buckets" {
  command = plan

  variables {
    nix_cache_bucket_name   = "mc-shared-cache"
    bazel_cache_bucket_name = "mc-shared-cache"
  }

  expect_failures = [google_compute_instance.workstation]
}

run "reject_unbounded_idle_shutdown" {
  command = plan

  variables {
    idle_shutdown_minutes = 0
  }

  expect_failures = [var.idle_shutdown_minutes]
}

run "reject_module_owned_metadata_override" {
  command = plan

  variables {
    metadata = {
      "enable-oslogin" = "FALSE"
    }
  }

  expect_failures = [var.metadata]
}

run "reject_public_data_classification" {
  command = plan

  variables {
    data_classification = "public"
  }

  expect_failures = [var.data_classification]
}

run "reject_local_ssd_on_unsupported_machine_type" {
  command = plan

  variables {
    machine_type    = "t2d-standard-16"
    local_ssd_count = 2
  }

  expect_failures = [google_compute_instance.workstation]
}

# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

mock_provider "google" {}

override_resource {
  target          = google_service_account.nodes
  override_during = plan
  values = {
    email = "gke-general-compute@mindclade-production.iam.gserviceaccount.com"
  }
}

variables {
  project_id               = "mindclade-production"
  cluster_name             = "mindclade-platform"
  name                     = "general-compute"
  region                   = "us-central1"
  node_locations           = ["us-central1-a", "us-central1-b"]
  pod_secondary_range_name = "gke-pods"
  service_account_id       = "gke-general-compute"
  environment              = "production"
  owner                    = "cloud-platform"
}

run "general_purpose_pool_contract" {
  command = plan

  variables {
    node_labels = {
      "scheduling.mindclade.dev/workload-class" = "build_cpu.1"
    }
    additional_taints = [{
      key    = "scheduling.mindclade.dev/dedicated"
      value  = "build_cpu.1"
      effect = "NO_EXECUTE"
    }]
  }

  assert {
    condition = (
      google_container_node_pool.this.autoscaling[0].total_min_node_count == 1 &&
      google_container_node_pool.this.autoscaling[0].total_max_node_count == 10 &&
      google_container_node_pool.this.autoscaling[0].location_policy == "BALANCED" &&
      google_container_node_pool.this.management[0].auto_repair == true &&
      google_container_node_pool.this.management[0].auto_upgrade == true &&
      google_container_node_pool.this.upgrade_settings[0].max_surge == 1 &&
      google_container_node_pool.this.node_drain_config[0].respect_pdb_during_node_pool_deletion == true &&
      google_container_node_pool.this.deletion_policy == "PREVENT"
    )
    error_message = "CPU capacity must be bounded, balanced, self-repairing, automatically upgraded, and deletion protected."
  }

  assert {
    condition = (
      google_container_node_pool.this.node_config[0].machine_type == "n2-standard-8" &&
      google_container_node_pool.this.node_config[0].service_account == google_service_account.nodes.email &&
      google_container_node_pool.this.node_config[0].shielded_instance_config[0].enable_secure_boot == true &&
      google_container_node_pool.this.node_config[0].shielded_instance_config[0].enable_integrity_monitoring == true &&
      google_container_node_pool.this.node_config[0].workload_metadata_config[0].mode == "GKE_METADATA" &&
      google_container_node_pool.this.network_config[0].enable_private_nodes == true
    )
    error_message = "General-purpose nodes must use the reviewed machine, dedicated identity, GKE metadata, Shielded VM, and private networking."
  }

  assert {
    condition = (
      google_service_account.nodes.account_id == "gke-general-compute" &&
      google_service_account.nodes.deletion_policy == "PREVENT" &&
      google_project_iam_member.nodes["roles/container.defaultNodeServiceAccount"].role == "roles/container.defaultNodeServiceAccount" &&
      google_container_node_pool.this.node_config[0].metadata["disable-legacy-endpoints"] == "true" &&
      google_container_node_pool.this.node_config[0].labels["mindclade.dev/workload-profile"] == "general-purpose" &&
      google_container_node_pool.this.node_config[0].labels["scheduling.mindclade.dev/workload-class"] == "build_cpu.1" &&
      one([for taint in output.node_taints : taint if taint.key == "scheduling.mindclade.dev/dedicated"]).effect == "NO_EXECUTE"
    )
    error_message = "The pool must use a minimally privileged dedicated node identity and immutable baseline metadata."
  }
}

run "high_memory_pool_contract" {
  command = plan

  variables {
    name               = "high-memory"
    service_account_id = "gke-high-memory"
    profile            = "HIGH_MEMORY"
  }

  assert {
    condition = (
      output.machine_type == "n2-highmem-8" &&
      one([for taint in output.node_taints : taint if taint.key == "scheduling.mindclade.dev/high-memory"]).effect == "NO_SCHEDULE"
    )
    error_message = "The high-memory profile must select a high-memory machine and isolate scheduling."
  }
}

run "acknowledged_spot_pool_contract" {
  command = plan

  variables {
    name               = "spot-compute"
    service_account_id = "gke-spot-compute"
    capacity_type      = "SPOT"
    spot_approval      = "I ACCEPT EVICTION AND CAPACITY-LOSS RISK"
    total_min_nodes    = 0
  }

  assert {
    condition = (
      google_container_node_pool.this.node_config[0].spot == true &&
      google_container_node_pool.this.autoscaling[0].location_policy == "ANY" &&
      one([for taint in output.node_taints : taint if taint.key == "scheduling.mindclade.dev/spot"]).effect == "NO_SCHEDULE"
    )
    error_message = "Spot capacity must scale from zero, diversify placement, and remain explicitly tainted."
  }
}

run "reject_spot_without_exact_approval" {
  command = plan

  variables {
    capacity_type   = "SPOT"
    total_min_nodes = 0
  }

  expect_failures = [google_container_node_pool.this]
}

run "reject_spot_reliable_baseline" {
  command = plan

  variables {
    capacity_type   = "SPOT"
    spot_approval   = "I ACCEPT EVICTION AND CAPACITY-LOSS RISK"
    total_min_nodes = 1
  }

  expect_failures = [google_container_node_pool.this]
}

run "reject_profile_machine_mismatch" {
  command = plan

  variables {
    profile      = "HIGH_MEMORY"
    machine_type = "n2-standard-8"
  }

  expect_failures = [google_container_node_pool.this]
}

run "reject_zone_outside_cluster_region" {
  command = plan

  variables {
    node_locations = ["us-east1-b"]
  }

  expect_failures = [google_container_node_pool.this]
}

run "reject_duplicate_managed_taint" {
  command = plan

  variables {
    profile = "HIGH_MEMORY"
    additional_taints = [{
      key    = "scheduling.mindclade.dev/high-memory"
      value  = "duplicate"
      effect = "NO_EXECUTE"
    }]
  }

  expect_failures = [google_container_node_pool.this]
}

run "reject_invalid_kubernetes_node_label" {
  command = plan

  variables {
    node_labels = {
      "Invalid Prefix/workload" = "cpu"
    }
  }

  expect_failures = [var.node_labels]
}

run "reject_cross_region_boot_disk_cmek" {
  command = plan

  variables {
    boot_disk_kms_key = "projects/mindclade-security/locations/us-east1/keyRings/gke/cryptoKeys/nodes"
  }

  expect_failures = [var.boot_disk_kms_key]
}

run "reject_invalid_autoscaling_bounds" {
  command = plan

  variables {
    total_min_nodes = 4
    total_max_nodes = 3
  }

  expect_failures = [google_container_node_pool.this]
}

run "reject_unsafe_upgrade_configuration" {
  command = plan

  variables {
    upgrade_max_surge       = 0
    upgrade_max_unavailable = 0
  }

  expect_failures = [google_container_node_pool.this]
}

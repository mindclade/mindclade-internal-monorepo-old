# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

mock_provider "google" {}

variables {
  project_id                 = "mindclade-production"
  cluster_name               = "mindclade-platform"
  name                       = "nova-h100-validation"
  region                     = "us-central1"
  zone                       = "us-central1-a"
  profile                    = "gke-h100-a3-megagpu-8g"
  node_service_account_email = "gpu-nodes@mindclade-production.iam.gserviceaccount.com"
  pod_secondary_range_name   = "gke-pods"
  environment                = "production"
  owner                      = "ml-platform"
}

run "isolated_h100_pool_contract" {
  command = plan

  variables {
    project_id                 = "mindclade-production"
    cluster_name               = "mindclade-platform"
    name                       = "nova-h100"
    region                     = "us-central1"
    zone                       = "us-central1-a"
    profile                    = "gke-h100-a3-megagpu-8g"
    node_service_account_email = "gpu-nodes@mindclade-production.iam.gserviceaccount.com"
    pod_secondary_range_name   = "gke-pods"
    environment                = "production"
    owner                      = "ml-platform"
    capacity_mode              = "ON_DEMAND"
    node_labels = {
      "scheduling.mindclade.dev/workload-class" = "NOVA_gpu.1"
      "empty-value"                             = ""
    }
    additional_taints = [{
      key    = "scheduling.mindclade.dev/dedicated"
      value  = "NOVA_gpu.1"
      effect = "NO_EXECUTE"
    }]
  }

  assert {
    condition = (
      contains(google_container_node_pool.this.node_locations, "us-central1-a") &&
      google_container_node_pool.this.autoscaling[0].total_min_node_count == 0 &&
      google_container_node_pool.this.autoscaling[0].total_max_node_count == 2 &&
      google_container_node_pool.this.management[0].auto_repair == true &&
      google_container_node_pool.this.deletion_policy == "PREVENT"
    )
    error_message = "GPU capacity must stay zonal, bounded, scale-to-zero capable, and deletion protected."
  }

  assert {
    condition = (
      google_container_node_pool.this.node_config[0].machine_type == "a3-megagpu-8g" &&
      google_container_node_pool.this.node_config[0].guest_accelerator[0].type == "nvidia-h100-mega-80gb" &&
      google_container_node_pool.this.node_config[0].guest_accelerator[0].count == 8 &&
      google_container_node_pool.this.node_config[0].guest_accelerator[0].gpu_driver_installation_config[0].gpu_driver_version == "DEFAULT" &&
      google_container_node_pool.this.node_config[0].gcfs_config[0].enabled == true
    )
    error_message = "The admitted H100 profile must map to the reviewed machine, accelerator count, managed driver, and image streaming."
  }

  assert {
    condition = (
      google_container_node_pool.this.node_config[0].shielded_instance_config[0].enable_secure_boot == true &&
      google_container_node_pool.this.node_config[0].workload_metadata_config[0].mode == "GKE_METADATA" &&
      google_container_node_pool.this.node_config[0].taint[0].key == "nvidia.com/gpu"
    )
    error_message = "GPU nodes must remain shielded, keyless, and tainted away from ordinary workloads."
  }

  assert {
    condition = (
      google_container_node_pool.this.node_config[0].labels["scheduling.mindclade.dev/workload-class"] == "NOVA_gpu.1" &&
      google_container_node_pool.this.node_config[0].labels["empty-value"] == "" &&
      one([
        for taint in google_container_node_pool.this.node_config[0].taint : taint
        if taint.key == "scheduling.mindclade.dev/dedicated"
      ]).value == "NOVA_gpu.1"
    )
    error_message = "Valid qualified Kubernetes keys, mixed permitted value characters, and empty label values must reach the node configuration."
  }
}

run "queued_capacity_must_scale_from_zero" {
  command = plan

  variables {
    project_id                 = "mindclade-production"
    cluster_name               = "mindclade-platform"
    name                       = "nova-h200-queued"
    region                     = "us-central1"
    zone                       = "us-central1-a"
    profile                    = "gke-h200-a3-ultragpu-8g"
    node_service_account_email = "gpu-nodes@mindclade-production.iam.gserviceaccount.com"
    pod_secondary_range_name   = "gke-pods"
    environment                = "production"
    owner                      = "ml-platform"
    capacity_mode              = "QUEUED_PROVISIONING"
    total_min_nodes            = 1
  }

  expect_failures = [google_container_node_pool.this]
}

run "reject_h200_standard_on_demand_capacity" {
  command = plan

  variables {
    project_id                 = "mindclade-production"
    cluster_name               = "mindclade-platform"
    name                       = "nova-h200-on-demand"
    region                     = "us-central1"
    zone                       = "us-central1-a"
    profile                    = "gke-h200-a3-ultragpu-8g"
    node_service_account_email = "gpu-nodes@mindclade-production.iam.gserviceaccount.com"
    pod_secondary_range_name   = "gke-pods"
    environment                = "production"
    owner                      = "ml-platform"
    capacity_mode              = "ON_DEMAND"
  }

  expect_failures = [google_container_node_pool.this]
}

run "queued_capacity_contract" {
  command = plan

  variables {
    project_id                 = "mindclade-production"
    cluster_name               = "mindclade-platform"
    name                       = "nova-h200-queued-valid"
    region                     = "us-central1"
    zone                       = "us-central1-a"
    profile                    = "gke-h200-a3-ultragpu-8g"
    node_service_account_email = "gpu-nodes@mindclade-production.iam.gserviceaccount.com"
    pod_secondary_range_name   = "gke-pods"
    environment                = "production"
    owner                      = "ml-platform"
    capacity_mode              = "QUEUED_PROVISIONING"
    enable_compact_placement   = false
    max_run_duration           = "172800s"
  }

  assert {
    condition = (
      google_container_node_pool.this.queued_provisioning[0].enabled == true &&
      google_container_node_pool.this.node_config[0].flex_start == true &&
      google_container_node_pool.this.node_config[0].max_run_duration == "172800s" &&
      google_container_node_pool.this.node_config[0].reservation_affinity[0].consume_reservation_type == "NO_RESERVATION" &&
      length(google_container_node_pool.this.placement_policy) == 0
    )
    error_message = "Queued capacity must combine queued provisioning with Flex Start, a bounded lifetime, explicit no-reservation affinity, and no compact placement policy."
  }
}

run "b200_queued_capacity_contract" {
  command = plan

  variables {
    name                     = "nova-b200-queued"
    profile                  = "gke-b200-a4-highgpu-8g"
    capacity_mode            = "QUEUED_PROVISIONING"
    enable_compact_placement = false
    max_run_duration         = "172800s"
  }

  assert {
    condition = (
      google_container_node_pool.this.node_config[0].machine_type == "a4-highgpu-8g" &&
      google_container_node_pool.this.node_config[0].guest_accelerator[0].type == "nvidia-b200" &&
      google_container_node_pool.this.node_config[0].guest_accelerator[0].count == 8 &&
      google_container_node_pool.this.queued_provisioning[0].enabled == true &&
      length(google_container_node_pool.this.placement_policy) == 0
    )
    error_message = "The B200 profile must select A4, eight B200 accelerators, queued capacity, and no incompatible compact placement."
  }
}

run "reject_b200_standard_on_demand_capacity" {
  command = plan

  variables {
    name          = "nova-b200-on-demand"
    profile       = "gke-b200-a4-highgpu-8g"
    capacity_mode = "ON_DEMAND"
  }

  expect_failures = [google_container_node_pool.this]
}

run "reject_standalone_flex_start_without_preview_approval" {
  command = plan

  variables {
    project_id                 = "mindclade-production"
    cluster_name               = "mindclade-platform"
    name                       = "nova-h100-flex-preview"
    region                     = "us-central1"
    zone                       = "us-central1-a"
    profile                    = "gke-h100-a3-megagpu-8g"
    node_service_account_email = "gpu-nodes@mindclade-production.iam.gserviceaccount.com"
    pod_secondary_range_name   = "gke-pods"
    environment                = "production"
    owner                      = "ml-platform"
    capacity_mode              = "FLEX_START"
    enable_compact_placement   = false
  }

  expect_failures = [google_container_node_pool.this]
}

run "reject_cross_region_boot_disk_cmek" {
  command = plan

  variables {
    project_id                 = "mindclade-production"
    cluster_name               = "mindclade-platform"
    name                       = "nova-h100-invalid-cmek"
    region                     = "us-central1"
    zone                       = "us-central1-a"
    profile                    = "gke-h100-a3-megagpu-8g"
    node_service_account_email = "gpu-nodes@mindclade-production.iam.gserviceaccount.com"
    pod_secondary_range_name   = "gke-pods"
    environment                = "production"
    owner                      = "ml-platform"
    boot_disk_kms_key          = "projects/mindclade-security/locations/us-east1/keyRings/gpu/cryptoKeys/boot"
  }

  expect_failures = [var.boot_disk_kms_key]
}

run "h100_boot_disk_cmek_uses_supported_pd_ssd" {
  command = plan

  variables {
    project_id                 = "mindclade-production"
    cluster_name               = "mindclade-platform"
    name                       = "nova-h100-cmek"
    region                     = "us-central1"
    zone                       = "us-central1-a"
    profile                    = "gke-h100-a3-megagpu-8g"
    node_service_account_email = "gpu-nodes@mindclade-production.iam.gserviceaccount.com"
    pod_secondary_range_name   = "gke-pods"
    environment                = "production"
    owner                      = "ml-platform"
    boot_disk_kms_key          = "projects/mindclade-security/locations/us-central1/keyRings/gpu/cryptoKeys/boot"
  }

  assert {
    condition     = google_container_node_pool.this.node_config[0].boot_disk[0].disk_type == "pd-ssd"
    error_message = "H100 boot-disk CMEK must select the GKE-supported pd-ssd disk type."
  }
}

run "reject_h200_boot_disk_cmek" {
  command = plan

  variables {
    project_id                 = "mindclade-production"
    cluster_name               = "mindclade-platform"
    name                       = "nova-h200-cmek"
    region                     = "us-central1"
    zone                       = "us-central1-a"
    profile                    = "gke-h200-a3-ultragpu-8g"
    node_service_account_email = "gpu-nodes@mindclade-production.iam.gserviceaccount.com"
    pod_secondary_range_name   = "gke-pods"
    environment                = "production"
    owner                      = "ml-platform"
    boot_disk_kms_key          = "projects/mindclade-security/locations/us-central1/keyRings/gpu/cryptoKeys/boot"
  }

  expect_failures = [google_container_node_pool.this]
}

run "reject_node_label_with_invalid_dns_prefix" {
  command = plan

  variables {
    node_labels = {
      "bad..prefix.example/workload" = "gpu"
    }
  }

  expect_failures = [var.node_labels]
}

run "reject_node_label_name_over_63_characters" {
  command = plan

  variables {
    node_labels = {
      "scheduling.mindclade.dev/${join("", [for _ in range(64) : "a"])}" = "gpu"
    }
  }

  expect_failures = [var.node_labels]
}

run "reject_node_labels_over_gke_aggregate_limit" {
  command = plan

  variables {
    node_labels = {
      for index in range(4) :
      "${join(".", concat([for _ in range(27) : "abcdefgh"], ["abcdef-${index}"]))}/workload" => join("", [for _ in range(63) : "v"])
    }
  }

  expect_failures = [var.node_labels]
}

run "reject_node_label_with_invalid_value" {
  command = plan

  variables {
    node_labels = {
      "scheduling.mindclade.dev/workload" = "-gpu"
    }
  }

  expect_failures = [var.node_labels]
}

run "reject_taint_with_invalid_dns_prefix" {
  command = plan

  variables {
    additional_taints = [{
      key    = "Bad.Prefix.example/dedicated"
      value  = "gpu"
      effect = "NO_SCHEDULE"
    }]
  }

  expect_failures = [var.additional_taints]
}

run "reject_taint_with_invalid_value" {
  command = plan

  variables {
    additional_taints = [{
      key    = "scheduling.mindclade.dev/dedicated"
      value  = "gpu/value"
      effect = "NO_EXECUTE"
    }]
  }

  expect_failures = [var.additional_taints]
}

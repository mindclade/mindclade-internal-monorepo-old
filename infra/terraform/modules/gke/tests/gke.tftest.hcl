# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

mock_provider "google" {}

run "private_regional_cluster_contract" {
  command = plan

  variables {
    project_id                        = "mindclade-production"
    name                              = "mindclade-platform"
    region                            = "us-central1"
    network                           = "projects/mindclade-host/global/networks/production"
    subnetwork                        = "projects/mindclade-host/regions/us-central1/subnetworks/gke"
    pod_secondary_range_name          = "gke-pods"
    service_secondary_range_name      = "gke-services"
    master_ipv4_cidr_block            = "172.16.0.0/28"
    system_node_service_account_email = "gke-nodes@mindclade-production.iam.gserviceaccount.com"
    rbac_security_group               = "gke-security-groups@example.com"
    environment                       = "production"
    owner                             = "cloud-platform"
    data_classification               = "restricted"
    database_encryption_key_name      = "projects/mindclade-security/locations/us-central1/keyRings/gke/cryptoKeys/secrets"
    master_authorized_networks = [{
      cidr_block   = "10.20.0.0/24"
      display_name = "platform-administration"
    }]
  }

  assert {
    condition = (
      google_container_cluster.this.location == "us-central1" &&
      google_container_cluster.this.min_master_version == "1.35.6-gke.1127000" &&
      google_container_cluster.this.release_channel[0].channel == "REGULAR" &&
      google_container_cluster.this.networking_mode == "VPC_NATIVE" &&
      google_container_cluster.this.datapath_provider == "ADVANCED_DATAPATH" &&
      length(google_container_cluster.this.network_policy) == 0 &&
      google_container_cluster.this.enable_intranode_visibility == true &&
      google_container_cluster.this.private_cluster_config[0].enable_private_nodes == true &&
      google_container_cluster.this.private_cluster_config[0].enable_private_endpoint == true
    )
    error_message = "The cluster must remain regional, VPC-native, Dataplane V2 without the API-incompatible legacy NetworkPolicy block, and private."
  }

  assert {
    condition = (
      google_container_cluster.this.addons_config[0].gcs_fuse_csi_driver_config[0].enabled == false &&
      google_container_node_pool.system.version == "1.35.6-gke.1127000"
    )
    error_message = "The training platform pin must remain exact and GCS Fuse must remain disabled."
  }

  assert {
    condition = (
      google_container_cluster.this.workload_identity_config[0].workload_pool == "mindclade-production.svc.id.goog" &&
      google_container_cluster.this.enable_legacy_abac == false &&
      google_container_cluster.this.enable_shielded_nodes == true &&
      google_container_cluster.this.binary_authorization[0].evaluation_mode == "PROJECT_SINGLETON_POLICY_ENFORCE"
    )
    error_message = "Identity and cluster security controls must remain enforced."
  }

  assert {
    condition = (
      google_container_cluster.this.monitoring_config[0].managed_prometheus[0].enabled == true &&
      contains(google_container_cluster.this.monitoring_config[0].enable_components, "DCGM")
    )
    error_message = "Managed Service for Prometheus and managed DCGM metrics must remain enabled."
  }

  assert {
    condition = (
      google_container_cluster.this.deletion_protection == true &&
      google_container_cluster.this.deletion_policy == "PREVENT" &&
      google_container_cluster.this.database_encryption[0].state == "ENCRYPTED" &&
      google_container_cluster.this.addons_config[0].gke_backup_agent_config[0].enabled == true
    )
    error_message = "Deletion, restricted-data encryption, and backup-agent controls must remain enabled."
  }

  assert {
    condition = (
      google_container_node_pool.system.autoscaling[0].total_min_node_count == 3 &&
      google_container_node_pool.system.management[0].auto_repair == true &&
      google_container_node_pool.system.management[0].auto_upgrade == true &&
      google_container_node_pool.system.node_config[0].workload_metadata_config[0].mode == "GKE_METADATA"
    )
    error_message = "The system node pool must remain redundant, managed, and Workload Identity aware."
  }
}

run "restricted_data_requires_application_layer_encryption" {
  command = plan

  variables {
    project_id                        = "mindclade-production"
    name                              = "restricted-canary"
    region                            = "us-central1"
    network                           = "production"
    subnetwork                        = "gke"
    pod_secondary_range_name          = "gke-pods"
    service_secondary_range_name      = "gke-services"
    master_ipv4_cidr_block            = "172.16.0.0/28"
    system_node_service_account_email = "gke-nodes@mindclade-production.iam.gserviceaccount.com"
    rbac_security_group               = "gke-security-groups@example.com"
    environment                       = "production"
    owner                             = "cloud-platform"
    data_classification               = "restricted"
    master_authorized_networks = [{
      cidr_block   = "10.20.0.0/24"
      display_name = "platform-administration"
    }]
  }

  expect_failures = [google_container_cluster.this]
}

run "reject_cross_region_database_encryption_key" {
  command = plan

  variables {
    project_id                        = "mindclade-production"
    name                              = "wrong-kms-region-canary"
    region                            = "us-central1"
    network                           = "production"
    subnetwork                        = "gke"
    pod_secondary_range_name          = "gke-pods"
    service_secondary_range_name      = "gke-services"
    master_ipv4_cidr_block            = "172.16.0.0/28"
    system_node_service_account_email = "gke-nodes@mindclade-production.iam.gserviceaccount.com"
    rbac_security_group               = "gke-security-groups@example.com"
    environment                       = "production"
    owner                             = "cloud-platform"
    data_classification               = "restricted"
    database_encryption_key_name      = "projects/mindclade-security/locations/global/keyRings/gke/cryptoKeys/secrets"
    master_authorized_networks = [{
      cidr_block   = "10.20.0.0/24"
      display_name = "platform-administration"
    }]
  }

  expect_failures = [var.database_encryption_key_name]
}

run "reject_public_control_plane_cidr" {
  command = plan

  variables {
    project_id                        = "mindclade-production"
    name                              = "public-master-cidr-canary"
    region                            = "us-central1"
    network                           = "production"
    subnetwork                        = "gke"
    pod_secondary_range_name          = "gke-pods"
    service_secondary_range_name      = "gke-services"
    master_ipv4_cidr_block            = "203.0.113.0/28"
    system_node_service_account_email = "gke-nodes@mindclade-production.iam.gserviceaccount.com"
    rbac_security_group               = "gke-security-groups@example.com"
    environment                       = "production"
    owner                             = "cloud-platform"
    master_authorized_networks = [{
      cidr_block   = "10.20.0.0/24"
      display_name = "platform-administration"
    }]
  }

  expect_failures = [var.master_ipv4_cidr_block]
}

locals {
  baseline_resource_labels = {
    data-classification = var.data_classification
    environment         = var.environment
    managed-by          = "terraform"
    owner               = var.owner
    system              = "mindclade"
  }

  resource_labels = merge(var.resource_labels, local.baseline_resource_labels)
}

resource "google_container_cluster" "this" {
  #checkov:skip=CKV_GCP_12:Dataplane V2 enforces Kubernetes NetworkPolicy natively; GKE rejects an explicit network_policy block with ADVANCED_DATAPATH.
  #checkov:skip=CKV_GCP_69:The provider check inspects only an inline default node_config; this cluster removes that pool and its tested system pool enforces GKE_METADATA.
  project     = var.project_id
  name        = var.name
  description = "Mindclade ${var.environment} regional GKE cluster"
  location    = var.region

  min_master_version = var.kubernetes_version

  network    = var.network
  subnetwork = var.subnetwork

  deletion_protection         = true
  deletion_policy             = "PREVENT"
  enable_fqdn_network_policy  = true
  enable_intranode_visibility = true
  enable_legacy_abac          = false
  enable_shielded_nodes       = true
  networking_mode             = "VPC_NATIVE"
  datapath_provider           = "ADVANCED_DATAPATH"

  initial_node_count       = 1
  remove_default_node_pool = true

  resource_labels = local.resource_labels

  ip_allocation_policy {
    cluster_secondary_range_name  = var.pod_secondary_range_name
    services_secondary_range_name = var.service_secondary_range_name
  }

  private_cluster_config {
    enable_private_nodes    = true
    enable_private_endpoint = true
    master_ipv4_cidr_block  = var.master_ipv4_cidr_block

    master_global_access_config {
      enabled = false
    }
  }

  master_authorized_networks_config {
    gcp_public_cidrs_access_enabled      = false
    private_endpoint_enforcement_enabled = true

    dynamic "cidr_blocks" {
      for_each = var.master_authorized_networks

      content {
        cidr_block   = cidr_blocks.value.cidr_block
        display_name = cidr_blocks.value.display_name
      }
    }
  }

  master_auth {
    client_certificate_config {
      issue_client_certificate = false
    }
  }

  workload_identity_config {
    workload_pool = "${var.project_id}.svc.id.goog"
  }

  authenticator_groups_config {
    security_group = var.rbac_security_group
  }

  dynamic "database_encryption" {
    for_each = var.database_encryption_key_name == null ? [] : [var.database_encryption_key_name]
    content {
      state    = "ENCRYPTED"
      key_name = database_encryption.value
    }
  }

  release_channel {
    channel = var.release_channel
  }

  maintenance_policy {
    recurring_window {
      start_time = var.maintenance_window.start_time
      end_time   = var.maintenance_window.end_time
      recurrence = var.maintenance_window.recurrence
    }
  }

  binary_authorization {
    evaluation_mode = "PROJECT_SINGLETON_POLICY_ENFORCE"
  }

  service_external_ips_config {
    enabled = false
  }

  addons_config {
    gce_persistent_disk_csi_driver_config {
      enabled = true
    }

    gcs_fuse_csi_driver_config {
      enabled = var.enable_gcs_fuse_csi_driver
    }

    gke_backup_agent_config {
      enabled = var.enable_backup_agent
    }

    horizontal_pod_autoscaling {
      disabled = false
    }

    http_load_balancing {
      disabled = false
    }
  }

  secret_manager_config {
    enabled = true
  }

  vertical_pod_autoscaling {
    enabled = true
  }

  cost_management_config {
    enabled = true
  }

  security_posture_config {
    mode               = "BASIC"
    vulnerability_mode = "VULNERABILITY_BASIC"
  }

  logging_config {
    enable_components = [
      "APISERVER",
      "CONTROLLER_MANAGER",
      "SCHEDULER",
      "SYSTEM_COMPONENTS",
      "WORKLOADS",
    ]
  }

  monitoring_config {
    enable_components = [
      "APISERVER",
      "CADVISOR",
      "CONTROLLER_MANAGER",
      "DAEMONSET",
      "DCGM",
      "DEPLOYMENT",
      "HPA",
      "JOBSET",
      "KUBELET",
      "POD",
      "SCHEDULER",
      "STATEFULSET",
      "STORAGE",
      "SYSTEM_COMPONENTS",
    ]

    managed_prometheus {
      enabled = true
    }

    advanced_datapath_observability_config {
      enable_metrics = true
      enable_relay   = false
    }
  }

  node_pool_defaults {
    node_config_defaults {
      insecure_kubelet_readonly_port_enabled = "FALSE"
      logging_variant                        = "DEFAULT"
    }
  }

  lifecycle {
    prevent_destroy = true

    precondition {
      condition     = var.system_node_pool_total_min_nodes <= var.system_node_pool_total_max_nodes
      error_message = "system_node_pool_total_min_nodes must not exceed system_node_pool_total_max_nodes."
    }

    precondition {
      condition     = var.data_classification != "restricted" || var.database_encryption_key_name != null
      error_message = "Restricted-data clusters require a Cloud KMS key for application-layer Kubernetes Secrets encryption."
    }

    precondition {
      condition     = var.release_channel == "REGULAR" && var.kubernetes_version == "1.35.6-gke.1127000"
      error_message = "The NOVA v1 training substrate is qualified only for GKE Regular 1.35.6-gke.1127000; update the immutable platform lock and qualification evidence before changing it."
    }
  }

  timeouts {
    create = "60m"
    update = "60m"
    delete = "60m"
  }
}

resource "google_container_node_pool" "system" {
  project  = var.project_id
  name     = "${var.name}-system"
  location = var.region
  cluster  = google_container_cluster.this.name
  version  = var.kubernetes_version

  deletion_policy           = "PREVENT"
  max_pods_per_node         = var.system_node_pool_max_pods_per_node
  ignore_node_count_changes = true

  autoscaling {
    total_min_node_count = var.system_node_pool_total_min_nodes
    total_max_node_count = var.system_node_pool_total_max_nodes
    location_policy      = "BALANCED"
  }

  management {
    auto_repair  = true
    auto_upgrade = true
  }

  upgrade_settings {
    max_surge       = 1
    max_unavailable = 0
    strategy        = "SURGE"
  }

  node_drain_config {
    grace_termination_duration            = "600s"
    pdb_timeout_duration                  = "600s"
    respect_pdb_during_node_pool_deletion = true
  }

  network_config {
    enable_private_nodes = true
    pod_range            = var.pod_secondary_range_name
  }

  node_config {
    image_type      = "COS_CONTAINERD"
    machine_type    = var.system_node_pool_machine_type
    service_account = var.system_node_service_account_email

    oauth_scopes = [
      "https://www.googleapis.com/auth/cloud-platform",
    ]

    labels = {
      "mindclade.dev/node-pool" = "system"
    }

    resource_labels = local.resource_labels

    metadata = {
      disable-legacy-endpoints = "true"
    }

    boot_disk {
      disk_type = "pd-balanced"
      size_gb   = var.system_node_pool_boot_disk_size_gb
    }

    shielded_instance_config {
      enable_integrity_monitoring = true
      enable_secure_boot          = true
    }

    workload_metadata_config {
      mode = "GKE_METADATA"
    }

    kubelet_config {
      cpu_cfs_quota                          = true
      insecure_kubelet_readonly_port_enabled = "FALSE"
      pod_pids_limit                         = 4096
    }
  }

  lifecycle {
    prevent_destroy = true
  }

  timeouts {
    create = "60m"
    update = "60m"
    delete = "60m"
  }
}

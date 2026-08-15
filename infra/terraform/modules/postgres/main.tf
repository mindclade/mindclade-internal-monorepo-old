locals {
  baseline_labels = {
    "data-classification" = var.data_classification
    environment           = var.environment
    "managed-by"          = "terraform"
    owner                 = var.owner
  }
}

resource "google_sql_database_instance" "primary" {
  #checkov:skip=CKV_GCP_79:PostgreSQL 17 is the repository-qualified application contract; major-version promotion requires migration and restore qualification.
  project                              = var.project_id
  name                                 = var.name
  region                               = var.region
  database_version                     = var.database_version
  encryption_key_name                  = var.kms_key_name
  deletion_protection                  = true
  deletion_policy                      = "PREVENT"
  enforce_new_sql_network_architecture = true

  settings {
    tier                        = var.tier
    edition                     = var.edition
    availability_type           = "REGIONAL"
    activation_policy           = "ALWAYS"
    disk_type                   = var.disk_type
    disk_size                   = var.disk_size_gb
    disk_autoresize             = true
    disk_autoresize_limit       = var.disk_autoresize_limit_gb
    pricing_plan                = "PER_USE"
    connector_enforcement       = var.connector_enforcement
    deletion_protection_enabled = true
    user_labels                 = merge(var.labels, local.baseline_labels)

    backup_configuration {
      enabled                        = true
      point_in_time_recovery_enabled = true
      start_time                     = var.backup_start_time_utc
      location                       = var.backup_location
      transaction_log_retention_days = var.transaction_log_retention_days

      backup_retention_settings {
        retained_backups = var.retained_backups
        retention_unit   = "COUNT"
      }
    }

    final_backup_config {
      enabled        = true
      retention_days = var.final_backup_retention_days
    }

    ip_configuration {
      ipv4_enabled                                  = false
      private_network                               = var.private_network
      allocated_ip_range                            = var.allocated_ip_range
      enable_private_path_for_google_cloud_services = var.enable_private_google_access
      ssl_mode                                      = "TRUSTED_CLIENT_CERTIFICATE_REQUIRED"
    }

    insights_config {
      query_insights_enabled  = var.query_insights_enabled
      query_plans_per_minute  = var.query_plans_per_minute
      query_string_length     = var.query_string_length
      record_application_tags = var.query_insights_enabled
      record_client_address   = false
    }

    maintenance_window {
      day          = var.maintenance_day
      hour         = var.maintenance_hour_utc
      update_track = var.maintenance_update_track
    }

    database_flags {
      name  = "cloudsql.enable_pgaudit"
      value = "on"
    }

    database_flags {
      name  = "log_checkpoints"
      value = "on"
    }

    database_flags {
      name  = "log_connections"
      value = "on"
    }

    database_flags {
      name  = "log_disconnections"
      value = "on"
    }

    database_flags {
      name  = "log_hostname"
      value = "on"
    }

    database_flags {
      name  = "log_lock_waits"
      value = "on"
    }

    database_flags {
      name  = "log_min_error_statement"
      value = "error"
    }

    database_flags {
      name  = "log_statement"
      value = "ddl"
    }

    dynamic "database_flags" {
      for_each = var.database_flags
      content {
        name  = database_flags.key
        value = database_flags.value
      }
    }
  }

  lifecycle {
    prevent_destroy = true
    ignore_changes  = [settings[0].disk_size]

    precondition {
      condition     = var.disk_autoresize_limit_gb == 0 || var.disk_autoresize_limit_gb >= var.disk_size_gb
      error_message = "disk_autoresize_limit_gb must be zero (service maximum) or at least disk_size_gb."
    }
  }
}

resource "google_sql_database" "this" {
  for_each = var.databases

  project         = var.project_id
  instance        = google_sql_database_instance.primary.name
  name            = each.key
  charset         = each.value.charset
  collation       = each.value.collation
  deletion_policy = "PREVENT"

  lifecycle {
    prevent_destroy = true
  }
}

resource "google_sql_user" "iam" {
  for_each = { for user in var.iam_database_users : user.name => user }

  project         = var.project_id
  instance        = google_sql_database_instance.primary.name
  name            = each.value.name
  type            = each.value.type
  deletion_policy = "PREVENT"

  lifecycle {
    prevent_destroy = true
  }
}

resource "google_sql_database_instance" "replica" {
  #checkov:skip=CKV_GCP_79:Read replicas must exactly match the repository-qualified PostgreSQL 17 primary before a separately qualified major-version promotion.
  for_each = var.read_replicas

  project                              = var.project_id
  name                                 = each.key
  region                               = each.value.region
  database_version                     = var.database_version
  master_instance_name                 = google_sql_database_instance.primary.name
  instance_type                        = "READ_REPLICA_INSTANCE"
  encryption_key_name                  = each.value.kms_key_name
  deletion_protection                  = true
  deletion_policy                      = "PREVENT"
  enforce_new_sql_network_architecture = true

  settings {
    tier                        = each.value.tier
    edition                     = var.edition
    availability_type           = "ZONAL"
    activation_policy           = "ALWAYS"
    disk_type                   = var.disk_type
    disk_autoresize             = true
    disk_autoresize_limit       = var.disk_autoresize_limit_gb
    connector_enforcement       = var.connector_enforcement
    deletion_protection_enabled = true
    user_labels                 = merge(var.labels, local.baseline_labels, { role = "read-replica" })

    ip_configuration {
      ipv4_enabled                                  = false
      private_network                               = each.value.private_network
      allocated_ip_range                            = each.value.allocated_ip_range
      enable_private_path_for_google_cloud_services = var.enable_private_google_access
      ssl_mode                                      = "TRUSTED_CLIENT_CERTIFICATE_REQUIRED"
    }

    insights_config {
      query_insights_enabled  = var.query_insights_enabled
      query_plans_per_minute  = var.query_plans_per_minute
      query_string_length     = var.query_string_length
      record_application_tags = var.query_insights_enabled
      record_client_address   = false
    }

    maintenance_window {
      day          = var.maintenance_day
      hour         = var.maintenance_hour_utc
      update_track = var.maintenance_update_track
    }

    database_flags {
      name  = "cloudsql.enable_pgaudit"
      value = "on"
    }

    database_flags {
      name  = "log_checkpoints"
      value = "on"
    }

    database_flags {
      name  = "log_connections"
      value = "on"
    }

    database_flags {
      name  = "log_disconnections"
      value = "on"
    }

    database_flags {
      name  = "log_hostname"
      value = "on"
    }

    database_flags {
      name  = "log_lock_waits"
      value = "on"
    }

    database_flags {
      name  = "log_min_error_statement"
      value = "error"
    }

    database_flags {
      name  = "log_statement"
      value = "ddl"
    }

    dynamic "database_flags" {
      for_each = var.database_flags
      content {
        name  = database_flags.key
        value = database_flags.value
      }
    }
  }

  lifecycle {
    prevent_destroy = true

    precondition {
      condition     = each.value.private_network == var.private_network
      error_message = "Cloud SQL read replicas must use the primary instance VPC."
    }
  }
}

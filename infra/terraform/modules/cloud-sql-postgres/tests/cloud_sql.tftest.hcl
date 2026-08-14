mock_provider "google" {}

run "private_ha_postgres_contract" {
  command = plan

  variables {
    project_id         = "mindclade-production"
    name               = "mindclade-control-plane"
    region             = "us-central1"
    private_network    = "projects/mindclade-host/global/networks/production"
    allocated_ip_range = "google-managed-services-production"
    backup_location    = "us"
    environment        = "production"
    owner              = "cloud-platform"
    databases = {
      control_plane = {}
    }
    iam_database_users = [{
      name = "runtime@mindclade-production.iam"
      type = "CLOUD_IAM_SERVICE_ACCOUNT"
    }]
    read_replicas = {
      "mindclade-control-plane-dr" = {
        region             = "us-east1"
        tier               = "db-custom-2-7680"
        private_network    = "projects/mindclade-host/global/networks/production"
        allocated_ip_range = "google-managed-services-production"
      }
    }
  }

  assert {
    condition = (
      google_sql_database_instance.primary.settings[0].availability_type == "REGIONAL" &&
      google_sql_database_instance.primary.settings[0].ip_configuration[0].ipv4_enabled == false &&
      google_sql_database_instance.primary.settings[0].ip_configuration[0].ssl_mode == "TRUSTED_CLIENT_CERTIFICATE_REQUIRED" &&
      google_sql_database_instance.primary.settings[0].connector_enforcement == "REQUIRED"
    )
    error_message = "The primary must remain regional, private, encrypted, and connector-only."
  }

  assert {
    condition = (
      google_sql_database_instance.primary.settings[0].backup_configuration[0].enabled == true &&
      google_sql_database_instance.primary.settings[0].backup_configuration[0].point_in_time_recovery_enabled == true &&
      google_sql_database_instance.primary.settings[0].backup_configuration[0].transaction_log_retention_days == 7 &&
      google_sql_database_instance.primary.settings[0].backup_configuration[0].backup_retention_settings[0].retained_backups == 14 &&
      google_sql_database_instance.primary.settings[0].final_backup_config[0].enabled == true
    )
    error_message = "Backup, PITR, retention, and final-backup controls must remain enabled."
  }

  assert {
    condition = (
      google_sql_database_instance.primary.deletion_protection == true &&
      google_sql_database_instance.primary.deletion_policy == "PREVENT" &&
      google_sql_database_instance.primary.settings[0].deletion_protection_enabled == true
    )
    error_message = "Cloud SQL deletion safeguards must be enforced at both Terraform and API layers."
  }

  assert {
    condition = (
      google_sql_database_instance.primary.settings[0].activation_policy == "ALWAYS" &&
      google_sql_database_instance.replica["mindclade-control-plane-dr"].settings[0].activation_policy == "ALWAYS"
    )
    error_message = "Primary and replica instances must remain fail-closed at activation policy ALWAYS."
  }

  assert {
    condition     = google_sql_database_instance.replica["mindclade-control-plane-dr"].region == "us-east1"
    error_message = "The optional recovery replica must be composed in a distinct region."
  }

  assert {
    condition     = length(google_sql_database_instance.replica["mindclade-control-plane-dr"].settings[0].final_backup_config) == 0
    error_message = "Read replicas must omit unsupported final-backup configuration."
  }

  assert {
    condition = contains([
      for flag in google_sql_database_instance.replica["mindclade-control-plane-dr"].settings[0].database_flags :
      "${flag.name}=${flag.value}"
    ], "cloudsql.iam_authentication=on")
    error_message = "Read replicas must explicitly enable the primary's IAM database-authentication flag."
  }
}

run "cmek_replica_contract" {
  command = plan

  variables {
    project_id      = "mindclade-production"
    name            = "mindclade-control-plane-cmek"
    region          = "us-central1"
    private_network = "projects/mindclade-host/global/networks/production"
    backup_location = "us"
    environment     = "production"
    owner           = "cloud-platform"
    kms_key_name    = "projects/mindclade-kms/locations/us-central1/keyRings/sql/cryptoKeys/control-plane"
    read_replicas = {
      "mindclade-control-plane-cmek-dr" = {
        region          = "us-east1"
        tier            = "db-custom-2-7680"
        private_network = "projects/mindclade-host/global/networks/production"
        kms_key_name    = "projects/mindclade-kms/locations/us-east1/keyRings/sql/cryptoKeys/control-plane-dr"
      }
    }
  }

  assert {
    condition = (
      google_sql_database_instance.primary.encryption_key_name == "projects/mindclade-kms/locations/us-central1/keyRings/sql/cryptoKeys/control-plane" &&
      google_sql_database_instance.replica["mindclade-control-plane-cmek-dr"].encryption_key_name == "projects/mindclade-kms/locations/us-east1/keyRings/sql/cryptoKeys/control-plane-dr"
    )
    error_message = "Primary and cross-region replicas must use region-local Cloud KMS keys."
  }
}

run "reject_replica_on_different_vpc" {
  command = plan

  variables {
    project_id      = "mindclade-production"
    name            = "mindclade-control-plane-invalid-vpc"
    region          = "us-central1"
    private_network = "projects/mindclade-host/global/networks/production"
    backup_location = "us"
    environment     = "production"
    owner           = "cloud-platform"
    read_replicas = {
      "mindclade-control-plane-invalid-vpc-dr" = {
        region          = "us-east1"
        tier            = "db-custom-2-7680"
        private_network = "projects/mindclade-host/global/networks/other"
      }
    }
  }

  expect_failures = [var.read_replicas]
}

run "reject_missing_replica_cmek" {
  command = plan

  variables {
    project_id      = "mindclade-production"
    name            = "mindclade-control-plane-invalid-cmek"
    region          = "us-central1"
    private_network = "projects/mindclade-host/global/networks/production"
    backup_location = "us"
    environment     = "production"
    owner           = "cloud-platform"
    kms_key_name    = "projects/mindclade-kms/locations/us-central1/keyRings/sql/cryptoKeys/control-plane"
    read_replicas = {
      "mindclade-control-plane-invalid-cmek-dr" = {
        region          = "us-east1"
        tier            = "db-custom-2-7680"
        private_network = "projects/mindclade-host/global/networks/production"
      }
    }
  }

  expect_failures = [var.read_replicas]
}

run "reject_unqualified_major_version" {
  command = plan

  variables {
    project_id       = "mindclade-production"
    name             = "mindclade-control-plane-pg18"
    region           = "us-central1"
    database_version = "POSTGRES_18"
    private_network  = "projects/mindclade-host/global/networks/production"
    backup_location  = "us"
    environment      = "production"
    owner            = "cloud-platform"
  }

  expect_failures = [var.database_version]
}

run "reject_unqualified_enterprise_plus_profile" {
  command = plan

  variables {
    project_id      = "mindclade-production"
    name            = "mindclade-control-plane-plus"
    region          = "us-central1"
    edition         = "ENTERPRISE_PLUS"
    private_network = "projects/mindclade-host/global/networks/production"
    backup_location = "us"
    environment     = "production"
    owner           = "cloud-platform"
  }

  expect_failures = [var.edition]
}

run "reject_duplicate_iam_database_user_name" {
  command = plan

  variables {
    project_id      = "mindclade-production"
    name            = "mindclade-control-plane-duplicate-user"
    region          = "us-central1"
    private_network = "projects/mindclade-host/global/networks/production"
    backup_location = "us"
    environment     = "production"
    owner           = "cloud-platform"
    iam_database_users = [
      {
        name = "runtime@example.com"
        type = "CLOUD_IAM_USER"
      },
      {
        name = "runtime@example.com"
        type = "CLOUD_IAM_GROUP"
      },
    ]
  }

  expect_failures = [var.iam_database_users]
}

run "reject_invalid_query_insights_bounds" {
  command = plan

  variables {
    project_id             = "mindclade-production"
    name                   = "mindclade-control-plane-query-bounds"
    region                 = "us-central1"
    private_network        = "projects/mindclade-host/global/networks/production"
    backup_location        = "us"
    environment            = "production"
    owner                  = "cloud-platform"
    query_plans_per_minute = 20.5
    query_string_length    = 4501
  }

  expect_failures = [var.query_plans_per_minute, var.query_string_length]
}

run "reject_invalid_custom_tier_and_disk_bounds" {
  command = plan

  variables {
    project_id               = "mindclade-production"
    name                     = "mindclade-control-plane-invalid-sizing"
    region                   = "us-central1"
    tier                     = "db-custom-3-1"
    disk_size_gb             = 65537
    disk_autoresize_limit_gb = 65537
    private_network          = "projects/mindclade-host/global/networks/production"
    backup_location          = "us"
    environment              = "production"
    owner                    = "cloud-platform"
  }

  expect_failures = [var.tier, var.disk_size_gb, var.disk_autoresize_limit_gb]
}

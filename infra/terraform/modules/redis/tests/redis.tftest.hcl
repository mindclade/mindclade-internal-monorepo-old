mock_provider "google" {}

run "private_ha_redis_contract" {
  command = plan

  variables {
    project_id         = "mindclade-production"
    name               = "control-plane-cache"
    region             = "us-central1"
    authorized_network = "projects/mindclade-host/global/networks/production"
    reserved_ip_range  = "google-managed-services-production"
    environment        = "production"
    owner              = "cloud-platform"
  }

  assert {
    condition = (
      google_redis_instance.this.tier == "STANDARD_HA" &&
      google_redis_instance.this.connect_mode == "PRIVATE_SERVICE_ACCESS" &&
      google_redis_instance.this.auth_enabled == true &&
      google_redis_instance.this.transit_encryption_mode == "SERVER_AUTHENTICATION"
    )
    error_message = "Redis must remain HA, private, authenticated, and encrypted in transit."
  }

  assert {
    condition = (
      google_redis_instance.this.deletion_protection == true &&
      google_redis_instance.this.deletion_policy == "PREVENT" &&
      google_redis_instance.this.persistence_config[0].persistence_mode == "RDB"
    )
    error_message = "Deletion safeguards and persistence must remain enabled."
  }
}

run "reject_cross_region_cmek" {
  command = plan

  variables {
    project_id         = "mindclade-production"
    name               = "control-plane-cache-invalid-cmek"
    region             = "us-central1"
    authorized_network = "projects/mindclade-host/global/networks/production"
    reserved_ip_range  = "google-managed-services-production"
    environment        = "production"
    owner              = "cloud-platform"
    kms_key_name       = "projects/mindclade-security/locations/us-east1/keyRings/redis/cryptoKeys/cache"
  }

  expect_failures = [var.kms_key_name]
}

run "reject_malformed_zone" {
  command = plan

  variables {
    project_id         = "mindclade-production"
    name               = "control-plane-cache-invalid-zone"
    region             = "us-central1"
    primary_zone       = "us-central1-not-a-zone"
    authorized_network = "projects/mindclade-host/global/networks/production"
    reserved_ip_range  = "google-managed-services-production"
    environment        = "production"
    owner              = "cloud-platform"
  }

  expect_failures = [var.primary_zone]
}

run "reject_oversized_instance" {
  command = plan

  variables {
    project_id         = "mindclade-production"
    name               = "control-plane-cache-oversized"
    region             = "us-central1"
    authorized_network = "projects/mindclade-host/global/networks/production"
    reserved_ip_range  = "google-managed-services-production"
    memory_size_gb     = 301
    environment        = "production"
    owner              = "cloud-platform"
  }

  expect_failures = [var.memory_size_gb]
}

run "reject_invalid_snapshot_start" {
  command = plan

  variables {
    project_id              = "mindclade-production"
    name                    = "control-plane-cache-invalid-snapshot"
    region                  = "us-central1"
    authorized_network      = "projects/mindclade-host/global/networks/production"
    reserved_ip_range       = "google-managed-services-production"
    rdb_snapshot_start_time = "tomorrow morning"
    environment             = "production"
    owner                   = "cloud-platform"
  }

  expect_failures = [var.rdb_snapshot_start_time]
}

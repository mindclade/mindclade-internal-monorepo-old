mock_provider "google" {}

run "metadata_only_secret_contract" {
  command = plan

  variables {
    project_id  = "mindclade-production"
    secret_id   = "control-plane-database"
    environment = "production"
    owner       = "cloud-platform"
    user_managed_replicas = {
      "us-central1" = "projects/security/locations/us-central1/keyRings/secrets/cryptoKeys/control-plane"
      "us-east1"    = "projects/security/locations/us-east1/keyRings/secrets/cryptoKeys/control-plane"
    }
    notification_topics = ["projects/mindclade-production/topics/secret-rotation"]
    rotation = {
      next_rotation_time = "2030-01-01T00:00:00Z"
      period_days        = 90
    }
    accessor_members = ["serviceAccount:runtime@mindclade-production.iam.gserviceaccount.com"]
  }

  assert {
    condition = (
      google_secret_manager_secret.this.deletion_protection == true &&
      google_secret_manager_secret.this.deletion_policy == "PREVENT" &&
      google_secret_manager_secret.this.version_destroy_ttl == "604800s"
    )
    error_message = "Secret and version deletion safeguards must remain enabled."
  }

  assert {
    condition     = length(google_secret_manager_secret.this.replication[0].user_managed[0].replicas) == 2
    error_message = "The user-managed production example must retain both regional replicas."
  }

  assert {
    condition     = google_secret_manager_secret_iam_member.this["roles/secretmanager.secretAccessor:serviceAccount:runtime@mindclade-production.iam.gserviceaccount.com"].role == "roles/secretmanager.secretAccessor"
    error_message = "Runtime access must use the payload-specific accessor role."
  }
}

run "rotation_requires_notification_handler" {
  command = plan

  variables {
    project_id          = "mindclade-development"
    secret_id           = "rotation-canary"
    environment         = "development"
    owner               = "cloud-platform"
    data_classification = "confidential"
    rotation = {
      next_rotation_time = "2030-01-01T00:00:00Z"
      period_days        = 30
    }
  }

  expect_failures = [google_secret_manager_secret.this]
}

run "reject_replica_cmek_location_mismatch" {
  command = plan

  variables {
    project_id  = "mindclade-production"
    secret_id   = "invalid-replica-key"
    environment = "production"
    owner       = "cloud-platform"
    user_managed_replicas = {
      "us-central1" = "projects/security/locations/us-east1/keyRings/secrets/cryptoKeys/control-plane"
      "us-east1"    = "projects/security/locations/us-east1/keyRings/secrets/cryptoKeys/control-plane"
    }
  }

  expect_failures = [var.user_managed_replicas]
}

run "reject_public_metadata_viewer" {
  command = plan

  variables {
    project_id          = "mindclade-development"
    secret_id           = "public-viewer-canary"
    environment         = "development"
    owner               = "cloud-platform"
    data_classification = "confidential"
    viewer_members      = ["allUsers"]
  }

  expect_failures = [var.viewer_members]
}

run "reject_restricted_secret_without_cmek" {
  command = plan

  variables {
    project_id  = "mindclade-production"
    secret_id   = "restricted-without-cmek"
    environment = "production"
    owner       = "cloud-platform"
  }

  expect_failures = [google_secret_manager_secret.this]
}

run "reject_shared_accessor_and_version_adder" {
  command = plan

  variables {
    project_id            = "mindclade-development"
    secret_id             = "shared-identity-canary"
    environment           = "development"
    owner                 = "cloud-platform"
    data_classification   = "confidential"
    accessor_members      = ["serviceAccount:rotation@mindclade-development.iam.gserviceaccount.com"]
    version_adder_members = ["serviceAccount:rotation@mindclade-development.iam.gserviceaccount.com"]
  }

  expect_failures = [google_secret_manager_secret.this]
}

run "reject_invalid_annotation_key" {
  command = plan

  variables {
    project_id          = "mindclade-development"
    secret_id           = "annotation-canary"
    environment         = "development"
    owner               = "cloud-platform"
    data_classification = "confidential"
    annotations = {
      "-invalid" = "operator metadata"
    }
  }

  expect_failures = [var.annotations]
}

run "reject_rotation_without_saved_plan_margin" {
  command = plan

  variables {
    project_id          = "mindclade-development"
    secret_id           = "rotation-plan-age-canary"
    environment         = "development"
    owner               = "cloud-platform"
    data_classification = "confidential"
    notification_topics = ["projects/mindclade-development/topics/secret-rotation"]
    rotation = {
      next_rotation_time = timeadd(timestamp(), "24h4m")
      period_days        = 30
    }
  }

  expect_failures = [google_secret_manager_secret.this]
}

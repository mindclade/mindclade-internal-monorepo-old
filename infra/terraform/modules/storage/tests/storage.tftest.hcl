mock_provider "google" {}

run "hardened_bucket_contract" {
  command = plan

  variables {
    project_id          = "mindclade-development"
    name                = "mindclade-development-artifacts"
    location            = "US-CENTRAL1"
    access_log_bucket   = "mindclade-central-storage-logs"
    environment         = "development"
    owner               = "cloud-platform"
    data_classification = "restricted"
    object_viewers      = ["serviceAccount:reader@mindclade-development.iam.gserviceaccount.com"]
  }

  assert {
    condition = (
      google_storage_bucket.this.uniform_bucket_level_access == true &&
      google_storage_bucket.this.public_access_prevention == "enforced" &&
      google_storage_bucket.this.force_destroy == false &&
      google_storage_bucket.this.deletion_policy == "PREVENT"
    )
    error_message = "Bucket access and deletion safeguards must remain enforced."
  }

  assert {
    condition = (
      google_storage_bucket.this.versioning[0].enabled == true &&
      google_storage_bucket.this.soft_delete_policy[0].retention_duration_seconds == 2592000
    )
    error_message = "Versioning and the 30-day soft-delete recovery window must be enabled by default."
  }

  assert {
    condition     = google_storage_bucket.this.labels["data-classification"] == "restricted"
    error_message = "Governance labels must take precedence."
  }

  assert {
    condition     = google_storage_bucket.this.logging[0].log_bucket == "mindclade-central-storage-logs"
    error_message = "Server-access logs must be routed to the separately governed log bucket."
  }

  assert {
    condition = (
      var.lifecycle_rules[0].action == "AbortIncompleteMultipartUpload" &&
      var.lifecycle_rules[0].age_days == 7 &&
      var.lifecycle_rules[0].with_state == null
    )
    error_message = "Incomplete multipart uploads must be aborted without an unsupported object-state condition."
  }

  assert {
    condition     = google_storage_bucket_iam_member.this["roles/storage.objectViewer:serviceAccount:reader@mindclade-development.iam.gserviceaccount.com"].role == "roles/storage.objectViewer"
    error_message = "Viewer IAM must be additive and role-scoped."
  }
}

run "reject_abort_with_object_state_filter" {
  command = plan

  variables {
    project_id        = "mindclade-development"
    name              = "mindclade-development-invalid-lifecycle"
    location          = "US-CENTRAL1"
    access_log_bucket = "mindclade-central-storage-logs"
    environment       = "development"
    owner             = "cloud-platform"
    lifecycle_rules = [{
      action     = "AbortIncompleteMultipartUpload"
      age_days   = 7
      with_state = "LIVE"
    }]
  }

  expect_failures = [var.lifecycle_rules]
}

run "nova_training_checkpoint_bucket_is_create_only" {
  command = plan

  variables {
    project_id           = "mindclade-development"
    name                 = "mindclade-development-nova-training-checkpoints"
    location             = "US-CENTRAL1"
    access_log_bucket    = "mindclade-central-storage-logs"
    environment          = "development"
    owner                = "inference-and-systems"
    data_classification  = "restricted"
    create_only_workload = true
    object_creators = [
      "principal://iam.googleapis.com/projects/123456789/locations/global/workloadIdentityPools/mindclade-development.svc.id.goog/subject/ns/mindclade-nova-training/sa/nova-v1-training",
    ]
    object_viewers = [
      "principal://iam.googleapis.com/projects/123456789/locations/global/workloadIdentityPools/mindclade-development.svc.id.goog/subject/ns/mindclade-nova-training/sa/nova-v1-training",
    ]
  }

  assert {
    condition = (
      google_storage_bucket.this.versioning[0].enabled == true &&
      length(google_storage_bucket_iam_member.this) == 2 &&
      alltrue([for binding in google_storage_bucket_iam_member.this : binding.role != "roles/storage.objectAdmin"])
    )
    error_message = "NOVA training checkpoints require versioned creator+viewer access without objectAdmin."
  }
}

run "reject_training_checkpoint_bucket_with_object_admin" {
  command = plan

  variables {
    project_id           = "mindclade-development"
    name                 = "mindclade-development-invalid-training-checkpoints"
    location             = "US-CENTRAL1"
    access_log_bucket    = "mindclade-central-storage-logs"
    environment          = "development"
    owner                = "inference-and-systems"
    create_only_workload = true
    object_creators      = ["serviceAccount:nova-training@mindclade-development.iam.gserviceaccount.com"]
    object_viewers       = ["serviceAccount:nova-training@mindclade-development.iam.gserviceaccount.com"]
    object_admins        = ["serviceAccount:nova-training@mindclade-development.iam.gserviceaccount.com"]
  }

  expect_failures = [google_storage_bucket.this]
}

run "reject_public_object_principal" {
  command = plan

  variables {
    project_id        = "mindclade-development"
    name              = "mindclade-development-public-principal"
    location          = "US-CENTRAL1"
    access_log_bucket = "mindclade-central-storage-logs"
    environment       = "development"
    owner             = "cloud-platform"
    object_viewers    = ["allUsers"]
  }

  expect_failures = [var.object_viewers]
}

run "reject_cross_location_cmek" {
  command = plan

  variables {
    project_id        = "mindclade-development"
    name              = "mindclade-development-invalid-cmek"
    location          = "US-CENTRAL1"
    access_log_bucket = "mindclade-central-storage-logs"
    environment       = "development"
    owner             = "cloud-platform"
    kms_key_name      = "projects/mindclade-security/locations/us-east1/keyRings/storage/cryptoKeys/artifacts"
  }

  expect_failures = [var.kms_key_name]
}

run "reject_irreversible_lock_without_confirmation" {
  command = plan

  variables {
    project_id               = "mindclade-production"
    name                     = "mindclade-production-records"
    location                 = "US"
    access_log_bucket        = "mindclade-central-storage-logs"
    environment              = "production"
    owner                    = "cloud-platform"
    retention_period_seconds = 2592000
    lock_retention_policy    = true
  }

  expect_failures = [google_storage_bucket.this]
}

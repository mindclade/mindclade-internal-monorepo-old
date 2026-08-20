# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

mock_provider "google" {}

variables {
  project_id                      = "mindclade-production"
  environment                     = "production"
  owner                           = "data-platform"
  storage_service_agent_email     = "service-123456789012@gs-project-accounts.iam.gserviceaccount.com"
  upstream_access_log_bucket_name = "mindclade-central-storage-access"
  access_log_bucket = {
    name         = "mindclade-production-storage-access"
    location     = "US"
    kms_key_name = "projects/security/locations/us/keyRings/data/cryptoKeys/storage"
  }
}

run "compose_access_data_and_create_only_ai_buckets" {
  command = plan

  variables {
    data_buckets = {
      curated = {
        name         = "mindclade-production-curated"
        location     = "US"
        kms_key_name = "projects/security/locations/us/keyRings/data/cryptoKeys/storage"
        data_class   = "curated"
        readers      = ["group:data-consumers@example.com"]
        writers      = ["serviceAccount:curator@mindclade-production.iam.gserviceaccount.com"]
      }
    }

    ai_artifact_buckets = {
      checkpoints = {
        name           = "mindclade-production-checkpoints"
        location       = "US"
        kms_key_name   = "projects/security/locations/us/keyRings/data/cryptoKeys/storage"
        artifact_class = "checkpoint"
        publishers     = ["serviceAccount:trainer@mindclade-production.iam.gserviceaccount.com"]
        readers        = ["serviceAccount:runtime@mindclade-production.iam.gserviceaccount.com"]
      }
    }
  }

  assert {
    condition = (
      module.access_logs.bucket.name == "mindclade-production-storage-access" &&
      module.data["curated"].bucket.name == "mindclade-production-curated" &&
      module.ai_artifacts["checkpoints"].bucket.name == "mindclade-production-checkpoints"
    )
    error_message = "The composition must delegate every explicit bucket class to the sibling storage module."
  }

  assert {
    condition = (
      output.access_log_bucket.writer_principal == "group:cloud-storage-analytics@google.com" &&
      output.access_log_bucket.retention_days == 365 &&
      output.ai_artifact_buckets["checkpoints"].create_only == true
    )
    error_message = "Access logs require a bounded writer/retention contract and AI artifacts must remain create-only."
  }

  assert {
    condition = (
      output.access_log_bucket.deletion_policy == "PREVENT" &&
      output.data_buckets["curated"].prevent_destroy == true &&
      output.ai_artifact_buckets["checkpoints"].prevent_destroy == true &&
      output.required_kms_grants["access_logs"].role == "roles/cloudkms.cryptoKeyEncrypterDecrypter"
    )
    error_message = "Every bucket class needs deletion guards and an explicit service-agent CMEK contract."
  }
}

run "reject_public_data_bucket_principal" {
  command = plan

  variables {
    data_buckets = {
      curated = {
        name         = "mindclade-production-curated"
        location     = "US"
        kms_key_name = "projects/security/locations/us/keyRings/data/cryptoKeys/storage"
        data_class   = "curated"
        readers      = ["allAuthenticatedUsers"]
      }
    }
  }

  expect_failures = [var.data_buckets]
}

run "reject_ai_bucket_without_publisher" {
  command = plan

  variables {
    ai_artifact_buckets = {
      models = {
        name           = "mindclade-production-models"
        location       = "US"
        kms_key_name   = "projects/security/locations/us/keyRings/data/cryptoKeys/storage"
        artifact_class = "model"
        publishers     = []
      }
    }
  }

  expect_failures = [var.ai_artifact_buckets]
}

run "reject_duplicate_managed_bucket_names" {
  command = plan

  variables {
    data_buckets = {
      evidence = {
        name         = "mindclade-production-collision"
        location     = "US"
        kms_key_name = "projects/security/locations/us/keyRings/data/cryptoKeys/storage"
        data_class   = "evidence"
      }
    }
    ai_artifact_buckets = {
      release = {
        name           = "mindclade-production-collision"
        location       = "US"
        kms_key_name   = "projects/security/locations/us/keyRings/data/cryptoKeys/storage"
        artifact_class = "release-evidence"
        publishers     = ["serviceAccount:release@mindclade-production.iam.gserviceaccount.com"]
      }
    }
  }

  expect_failures = [var.data_buckets]
}

run "reject_access_log_self_cycle" {
  command = plan

  variables {
    upstream_access_log_bucket_name = "mindclade-production-storage-access"
    data_buckets = {
      raw = {
        name         = "mindclade-production-raw"
        location     = "US"
        kms_key_name = "projects/security/locations/us/keyRings/data/cryptoKeys/storage"
        data_class   = "raw"
      }
    }
  }

  expect_failures = [var.data_buckets]
}

run "reject_empty_composition" {
  command = plan

  expect_failures = [var.data_buckets]
}

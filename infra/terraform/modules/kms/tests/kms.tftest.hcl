# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

mock_provider "google" {}

run "software_key_contract" {
  command = plan

  variables {
    project_id    = "mindclade-security-prod"
    location      = "us-central1"
    key_ring_name = "application-data"
    keys = {
      primary = {}
    }
  }

  assert {
    condition = (
      google_kms_crypto_key.this["primary"].purpose == "ENCRYPT_DECRYPT" &&
      google_kms_crypto_key.this["primary"].rotation_period == "7776000s" &&
      google_kms_crypto_key.this["primary"].destroy_scheduled_duration == "2592000s" &&
      google_kms_crypto_key.this["primary"].skip_initial_version_creation == false &&
      google_kms_crypto_key.this["primary"].deletion_policy == "PREVENT" &&
      google_kms_crypto_key.this["primary"].version_template[0].algorithm == "GOOGLE_SYMMETRIC_ENCRYPTION" &&
      google_kms_crypto_key.this["primary"].version_template[0].protection_level == "SOFTWARE"
    )
    error_message = "Default keys must be rotating symmetric keys with recoverable destruction and deletion safeguards."
  }
}

run "hsm_key_contract" {
  command = plan

  variables {
    project_id    = "mindclade-security-prod"
    location      = "us-central1"
    key_ring_name = "regulated-data"
    keys = {
      primary = {
        protection_level                   = "HSM"
        rotation_period_seconds            = 2592000
        destroy_scheduled_duration_seconds = 7776000
      }
    }
  }

  assert {
    condition = (
      google_kms_crypto_key.this["primary"].version_template[0].protection_level == "HSM" &&
      google_kms_crypto_key.this["primary"].rotation_period == "2592000s" &&
      google_kms_crypto_key.this["primary"].destroy_scheduled_duration == "7776000s"
    )
    error_message = "Explicit HSM, rotation, and recovery-window choices must be preserved."
  }
}

run "additive_encrypter_decrypter_contract" {
  command = plan

  variables {
    project_id    = "mindclade-security-prod"
    location      = "us-central1"
    key_ring_name = "application-data"
    keys = {
      storage = {}
    }
    encrypter_decrypters = {
      storage = [
        "serviceAccount:service-123456789012@gs-project-accounts.iam.gserviceaccount.com",
      ]
    }
  }

  assert {
    condition = (
      length(google_kms_crypto_key_iam_member.encrypter_decrypter) == 1 &&
      google_kms_crypto_key_iam_member.encrypter_decrypter["storage:serviceAccount:service-123456789012@gs-project-accounts.iam.gserviceaccount.com"].member == "serviceAccount:service-123456789012@gs-project-accounts.iam.gserviceaccount.com" &&
      google_kms_crypto_key_iam_member.encrypter_decrypter["storage:serviceAccount:service-123456789012@gs-project-accounts.iam.gserviceaccount.com"].role == "roles/cloudkms.cryptoKeyEncrypterDecrypter"
    )
    error_message = "Service-agent access must remain an additive member grant on the exact symmetric key."
  }
}

run "rejects_encrypter_decrypter_for_unknown_key" {
  command = plan

  variables {
    project_id    = "mindclade-security-prod"
    location      = "us-central1"
    key_ring_name = "application-data"
    keys = {
      storage = {}
    }
    encrypter_decrypters = {
      missing = [
        "serviceAccount:service-123456789012@gs-project-accounts.iam.gserviceaccount.com",
      ]
    }
  }

  expect_failures = [var.encrypter_decrypters]
}

run "rejects_public_encrypter_decrypter" {
  command = plan

  variables {
    project_id    = "mindclade-security-prod"
    location      = "us-central1"
    key_ring_name = "application-data"
    keys = {
      storage = {}
    }
    encrypter_decrypters = {
      storage = ["allAuthenticatedUsers"]
    }
  }

  expect_failures = [var.encrypter_decrypters]
}

run "rejects_short_destruction_window" {
  command = plan

  variables {
    project_id    = "mindclade-security-prod"
    location      = "us-central1"
    key_ring_name = "application-data"
    keys = {
      primary = {
        destroy_scheduled_duration_seconds = 3600
      }
    }
  }

  expect_failures = [var.keys]
}

run "rejects_merged_label_overflow" {
  command = plan

  variables {
    project_id    = "mindclade-security-prod"
    location      = "us-central1"
    key_ring_name = "application-data"
    labels = {
      for index in range(40) : "global_${index}" => "value"
    }
    keys = {
      primary = {
        labels = {
          for index in range(40) : "key_${index}" => "value"
        }
      }
    }
  }

  expect_failures = [google_kms_crypto_key.this["primary"]]
}

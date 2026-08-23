# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

mock_provider "google" {}

variables {
  qualification_state           = "qualified-v1"
  project_id                    = "mindclade-development"
  name                          = "mindclade-workstation-0123456789ab"
  source_uri                    = "https://storage.googleapis.com/mc-workstation-images/mindclade-workstation-x86-64-0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef.tar.gz"
  source_bucket_name            = "mc-workstation-images"
  source_object_generation      = "1740000000000000"
  source_sha256                 = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
  image_contract_sha256         = "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
  kms_key_name                  = "projects/mc-b-seed-fb7649/locations/us/keyRings/images/cryptoKeys/workstation"
  compute_service_account_email = "service-123456789012@compute-system.iam.gserviceaccount.com"
  environment                   = "development"
  owner                         = "platform"
}

run "immutable_image_contract" {
  command = plan

  assert {
    condition = (
      google_compute_image.this.deletion_policy == "PREVENT" &&
      google_compute_image.this.raw_disk[0].source == var.source_uri &&
      google_compute_image.this.raw_disk[0].container_type == "TAR" &&
      google_compute_image.this.image_encryption_key[0].kms_key_self_link == var.kms_key_name &&
      output.source_contract.object_generation == var.source_object_generation &&
      output.source_contract.image_contract_sha256 == var.image_contract_sha256 &&
      output.source_contract.bucket_name == var.source_bucket_name &&
      output.source_contract.content_addressed == true
    )
    error_message = "The image must bind one content-addressed raw disk, its retained GCS generation, CMEK, and deletion protection."
  }
}

run "reject_unqualified_source" {
  command = plan

  variables {
    qualification_state = "blocked"
  }

  expect_failures = [var.qualification_state]
}

run "reject_cross_bucket_source" {
  command = plan

  variables {
    source_bucket_name = "mc-other-workstation-images"
  }

  expect_failures = [google_compute_image.this]
}

run "reject_mutable_source_alias" {
  command = plan

  variables {
    source_uri = "https://storage.googleapis.com/mc-workstation-images/latest-0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef.tar.gz"
  }

  expect_failures = [google_compute_image.this]
}

run "reject_source_without_digest" {
  command = plan

  variables {
    source_uri = "https://storage.googleapis.com/mc-workstation-images/mindclade-workstation.tar.gz"
  }

  expect_failures = [google_compute_image.this]
}

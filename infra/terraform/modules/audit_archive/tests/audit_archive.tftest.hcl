# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

mock_provider "google" {}

variables {
  parent                      = "organizations/123456789012"
  project_id                  = "mindclade-audit"
  environment                 = "production"
  owner                       = "security"
  storage_service_agent_email = "service-123456789012@gs-project-accounts.iam.gserviceaccount.com"
  bucket_name                 = "mindclade-central-audit-archive"
  access_log_bucket_name      = "mindclade-central-storage-access"
  location                    = "US"
  kms_key_name                = "projects/mindclade-security/locations/us/keyRings/audit/cryptoKeys/archive"
  retention_lock_confirmation = "LOCKING A CLOUD STORAGE RETENTION POLICY IS IRREVERSIBLE"
}

run "immutable_complete_audit_contract" {
  command = plan

  assert {
    condition = (
      output.audit_contract.include_children == true &&
      output.audit_contract.exclusions == [] &&
      strcontains(output.audit_contract.filter, "cloudaudit.googleapis.com/activity") &&
      strcontains(output.audit_contract.filter, "cloudaudit.googleapis.com/system_event") &&
      strcontains(output.audit_contract.filter, "cloudaudit.googleapis.com/policy") &&
      strcontains(output.audit_contract.filter, "cloudaudit.googleapis.com/data_access")
    )
    error_message = "The immutable archive must cover every Cloud Audit Logs class with descendants and no exclusions."
  }

  assert {
    condition = (
      output.audit_contract.retention_days == 2555 &&
      output.audit_contract.retention_locked == true &&
      output.audit_contract.soft_delete_retention_days == 90 &&
      output.audit_contract.access_log_bucket_name == "mindclade-central-storage-access" &&
      output.audit_contract.deletion_policy == "PREVENT" &&
      output.audit_contract.terraform_prevent_destroy == true
    )
    error_message = "The central archive must retain locked retention, recovery, and deletion safeguards."
  }


  assert {
    condition = (
      output.required_access_log_writer_grant.member == "group:cloud-storage-analytics@google.com" &&
      output.required_access_log_writer_grant.role == "roles/storage.objectCreator"
    )
    error_message = "The access-log bucket owner needs the exact additive Storage analytics writer grant."
  }

  assert {
    condition = (
      output.required_kms_grant.member == "serviceAccount:service-123456789012@gs-project-accounts.iam.gserviceaccount.com" &&
      output.required_kms_grant.role == "roles/cloudkms.cryptoKeyEncrypterDecrypter"
    )
    error_message = "The key-owning state needs an explicit Cloud Storage service-agent CMEK grant."
  }

  assert {
    condition     = module.archive.default_bucket_scope == null
    error_message = "The destination project's _Default bucket must be left unmanaged unless explicitly requested."
  }
}

run "folder_parent_is_supported" {
  command = plan

  variables {
    parent = "folders/123456789012"
  }

  assert {
    condition     = output.audit_contract.parent == "folders/123456789012"
    error_message = "Folder-scoped archives must preserve the folder parent contract."
  }
}

run "reject_missing_irreversible_confirmation" {
  command = plan

  variables {
    retention_lock_confirmation = "not-approved"
  }

  expect_failures = [var.retention_lock_confirmation]
}

run "reject_short_audit_retention" {
  command = plan

  variables {
    retention_days = 30
  }

  expect_failures = [var.retention_days]
}

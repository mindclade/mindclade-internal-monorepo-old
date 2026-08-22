# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

output "buckets" {
  value = { for key, bucket in google_storage_bucket.this : key => { name = bucket.name, url = bucket.url, self_link = bucket.self_link } }
}
output "bucket_iam_members" {
  value = {
    for key, grant in google_storage_bucket_iam_member.reader : key => {
      bucket = grant.bucket
      role   = grant.role
      member = grant.member
    }
  }
}
output "deny_policy_names" {
  value = { for key, policy in google_iam_deny_policy.this : key => policy.name }
}
output "required_kms_grant" {
  value = { crypto_key = var.encryption_key, service_agent_template = "service-PROJECT_NUMBER@gs-project-accounts.iam.gserviceaccount.com", role = "roles/cloudkms.cryptoKeyEncrypterDecrypter" }
}

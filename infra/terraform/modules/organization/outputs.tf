# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

output "tag_keys" {
  description = "Created tag keys keyed by the caller-provided stable alias."
  value = {
    for alias, tag_key in google_tags_tag_key.this : alias => {
      id              = tag_key.id
      name            = tag_key.name
      namespaced_name = tag_key.namespaced_name
      short_name      = tag_key.short_name
    }
  }
}

output "tag_values" {
  description = "Created tag values keyed by <tag-key-alias>/<tag-value-alias>."
  value = {
    for reference, tag_value in google_tags_tag_value.this : reference => {
      id              = tag_value.id
      name            = tag_value.name
      namespaced_name = tag_value.namespaced_name
      short_name      = tag_value.short_name
    }
  }
}

output "tag_bindings" {
  description = "Protected tag bindings keyed by the caller-provided stable alias."
  value = {
    for alias, binding in google_tags_tag_binding.this : alias => {
      id        = binding.id
      parent    = binding.parent
      tag_value = binding.tag_value
    }
  }
}

output "iam_grants" {
  description = "Additive organization IAM grants keyed by the caller-provided stable alias."
  value = {
    for alias, grant in google_organization_iam_member.this : alias => {
      role   = grant.role
      member = grant.member
    }
  }
}

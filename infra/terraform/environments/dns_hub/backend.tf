# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

# PARTIAL backend configuration: the bucket is supplied at init time, not
# committed.
#
#   terraform init -backend-config=bucket=<state-bucket>
#
# The bucket name is the one piece of this root that is genuinely
# estate-specific and cannot be an input variable -- Terraform reads the backend
# block before it evaluates variables, so `bucket = var.x` is not expressible.
# Leaving it out is what keeps this directory forkable.
#
# The prefix IS committed, because it is the identity of this state rather than
# a property of whoever holds it. Two roots sharing a prefix silently adopt each
# other's resources.
#
# Use a bucket with versioning enabled. This state holds the delegation for
# every domain the estate owns; recovering it from a bad apply is a restore, and
# without object versioning there is nothing to restore from.
terraform {
  backend "gcs" {
    prefix = "dns-hub"
  }
}

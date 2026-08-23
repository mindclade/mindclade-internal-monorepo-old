# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

variable "project_id" {
  description = "Google Cloud project that owns the immutable Compute Image"
  type        = string

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{4,28}[a-z0-9]$", var.project_id))
    error_message = "project_id must be a valid Google Cloud project ID."
  }
}

variable "qualification_state" {
  description = "Explicit cross-repository evidence transition authorizing Compute Image creation"
  type        = string

  validation {
    condition     = var.qualification_state == "qualified-v1"
    error_message = "qualification_state must be qualified-v1 before Terraform may create an image."
  }
}

variable "name" {
  description = "Immutable image name; callers create a new name for every artifact digest"
  type        = string

  validation {
    condition     = can(regex("^[a-z](?:[-a-z0-9]{0,61}[a-z0-9])?$", var.name))
    error_message = "name must be a valid Compute Engine image name."
  }
}

variable "source_uri" {
  description = "Full HTTPS Cloud Storage URL of a disk.raw tar.gz object whose name contains its SHA-256"
  type        = string

  validation {
    condition     = can(regex("^https://storage[.]googleapis[.]com/[a-z0-9][a-z0-9._-]{1,61}[a-z0-9]/[A-Za-z0-9._/-]+[.]tar[.]gz$", var.source_uri))
    error_message = "source_uri must be a full https://storage.googleapis.com/<bucket>/<object>.tar.gz URL."
  }
}

variable "source_bucket_name" {
  description = "Exact Terraform-owned Cloud Storage bucket from which the raw disk may be imported"
  type        = string

  validation {
    condition     = can(regex("^[a-z0-9][a-z0-9._-]{1,61}[a-z0-9]$", var.source_bucket_name))
    error_message = "source_bucket_name must be a valid Cloud Storage bucket name."
  }
}

variable "source_object_generation" {
  description = "Generation of the create-only GCS source object retained as release evidence"
  type        = string

  validation {
    condition     = can(regex("^[1-9][0-9]{0,31}$", var.source_object_generation))
    error_message = "source_object_generation must be a positive decimal GCS object generation."
  }
}

variable "source_sha256" {
  description = "Lowercase SHA-256 digest of the compressed raw-disk artifact"
  type        = string

  validation {
    condition     = can(regex("^[a-f0-9]{64}$", var.source_sha256)) && var.source_sha256 != "0000000000000000000000000000000000000000000000000000000000000000"
    error_message = "source_sha256 must be a nonzero lowercase SHA-256 digest."
  }
}

variable "image_contract_sha256" {
  description = "Lowercase SHA-256 digest of the contract embedded in the source image"
  type        = string

  validation {
    condition     = can(regex("^[a-f0-9]{64}$", var.image_contract_sha256)) && var.image_contract_sha256 != "0000000000000000000000000000000000000000000000000000000000000000"
    error_message = "image_contract_sha256 must be a nonzero lowercase SHA-256 digest."
  }
}

variable "kms_key_name" {
  description = "CMEK crypto-key resource name encrypting the Compute Image"
  type        = string

  validation {
    condition     = can(regex("^projects/[a-z][a-z0-9-]{4,28}[a-z0-9]/locations/[^/]+/keyRings/[A-Za-z0-9_-]+/cryptoKeys/[A-Za-z0-9_-]+$", var.kms_key_name))
    error_message = "kms_key_name must be a complete Cloud KMS crypto-key resource name."
  }
}

variable "compute_service_account_email" {
  description = "Compute Engine service-agent email authorized on kms_key_name"
  type        = string

  validation {
    condition     = can(regex("^service-[0-9]{6,32}@compute-system[.]iam[.]gserviceaccount[.]com$", var.compute_service_account_email))
    error_message = "compute_service_account_email must be a Compute Engine service-agent email."
  }
}

variable "description" {
  description = "Non-sensitive purpose included before immutable source evidence"
  type        = string
  default     = "Mindclade immutable NixOS workstation image."

  validation {
    condition     = length(trimspace(var.description)) >= 4 && length(var.description) <= 512
    error_message = "description must contain 4-512 characters."
  }
}

variable "storage_locations" {
  description = "Locations where Compute Engine stores the encrypted image"
  type        = list(string)
  default     = ["us"]

  validation {
    condition     = length(var.storage_locations) >= 1 && length(var.storage_locations) <= 2 && alltrue([for location in var.storage_locations : can(regex("^(us|[a-z]+-[a-z0-9]+[0-9])$", location))])
    error_message = "storage_locations must contain one or two U.S. regional or multi-regional locations."
  }
}

variable "environment" {
  description = "Environment governance label"
  type        = string

  validation {
    condition     = can(regex("^[a-z0-9][a-z0-9_-]{0,62}$", var.environment))
    error_message = "environment must be a valid Google Cloud label value."
  }
}

variable "owner" {
  description = "Accountable team governance label"
  type        = string

  validation {
    condition     = can(regex("^[a-z0-9][a-z0-9_-]{0,62}$", var.owner))
    error_message = "owner must be a valid Google Cloud label value."
  }
}

variable "data_classification" {
  description = "Image governance classification; public is forbidden"
  type        = string
  default     = "internal"

  validation {
    condition     = contains(["internal", "confidential", "restricted"], var.data_classification)
    error_message = "data_classification must be internal, confidential, or restricted."
  }
}

variable "labels" {
  description = "Additional labels; baseline governance labels take precedence"
  type        = map(string)
  default     = {}

  validation {
    condition = length(var.labels) <= 58 && alltrue([
      for key, value in var.labels :
      can(regex("^[a-z][a-z0-9_-]{0,62}$", key)) &&
      can(regex("^$|^[a-z0-9][a-z0-9_-]{0,62}$", value))
    ])
    error_message = "labels must leave room for baseline labels and contain valid lowercase pairs."
  }
}

variable "project_id" {
  description = "Google Cloud project that owns the key ring"
  type        = string

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{4,28}[a-z0-9]$", var.project_id))
    error_message = "project_id must be a valid Google Cloud project ID."
  }
}

variable "location" {
  description = "Immutable Cloud KMS location for the key ring"
  type        = string

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{0,62}$", var.location))
    error_message = "location must be a valid lowercase Cloud KMS location identifier."
  }
}

variable "key_ring_name" {
  description = "Stable name for the Cloud KMS key ring"
  type        = string

  validation {
    condition     = can(regex("^[a-z](?:[-a-z0-9]{0,61}[a-z0-9])?$", var.key_ring_name))
    error_message = "key_ring_name must be a valid 1-63 character lowercase name."
  }
}

variable "labels" {
  description = "Non-sensitive labels merged into every CryptoKey"
  type        = map(string)
  default     = {}

  validation {
    condition = length(var.labels) <= 63 && alltrue([
      for key, value in var.labels :
      can(regex("^[a-z][a-z0-9_-]{0,62}$", key)) &&
      can(regex("^[a-z0-9_-]{0,63}$", value))
    ])
    error_message = "labels must leave room for managed-by and contain valid lowercase Google Cloud label pairs."
  }
}

variable "keys" {
  description = <<-EOT
    Symmetric encryption keys keyed by stable CryptoKey name.

    For ASYMMETRIC SIGNING keys — where the private key must never leave KMS — see
    `signing_keys` below. They are separate variables rather than one with a purpose field
    because almost nothing about them is shared: a signing key cannot carry an automatic
    rotation period, and treating them uniformly means a validation that permits an
    invalid combination on one of the two.
  EOT
  type = map(object({
    rotation_period_seconds            = optional(number, 7776000)
    destroy_scheduled_duration_seconds = optional(number, 2592000)
    protection_level                   = optional(string, "SOFTWARE")
    labels                             = optional(map(string), {})
  }))
  default = {}

  validation {
    condition = alltrue([
      for name, key in var.keys :
      can(regex("^[a-z](?:[-a-z0-9]{0,61}[a-z0-9])?$", name)) &&
      key.rotation_period_seconds >= 86400 &&
      key.rotation_period_seconds <= 7776000 &&
      floor(key.rotation_period_seconds) == key.rotation_period_seconds &&
      key.destroy_scheduled_duration_seconds >= 86400 &&
      key.destroy_scheduled_duration_seconds <= 10368000 &&
      floor(key.destroy_scheduled_duration_seconds) == key.destroy_scheduled_duration_seconds &&
      contains(["SOFTWARE", "HSM"], key.protection_level) &&
      length(key.labels) <= 63 && alltrue([
        for label_key, label_value in key.labels :
        can(regex("^[a-z][a-z0-9_-]{0,62}$", label_key)) &&
        can(regex("^[a-z0-9_-]{0,63}$", label_value))
      ])
    ])
    error_message = "keys require valid names, 1-90 day rotations, 1-120 day destruction delays, SOFTWARE or HSM protection, and valid labels."
  }
}

variable "signing_keys" {
  description = <<-EOT
    Asymmetric signing keys keyed by stable CryptoKey name.

    The distinguishing property is that the PRIVATE KEY NEVER LEAVES CLOUD KMS. Signing is an
    API call; the caller holds no key material, so a compromised pod cannot exfiltrate one.
    Verifiers fetch the public half and check signatures locally, with no call to KMS at all.

    That is what makes this the right shape for the highest-blast-radius credential in the
    estate — the token-signing key. Every other secret here is bounded by something else: a
    session key by the IAP assertion that must accompany it, a DNS credential by zone scope, a
    deploy key by being read-only. A leaked signing key is bounded by nothing, because it mints
    tokens for any principal against any audience. So it is the one that must not exist outside
    KMS in the first place.

    NO ROTATION PERIOD. Cloud KMS cannot automatically rotate an asymmetric key: rotation would
    create a new version, and every verifier holding the old public key would reject signatures
    made with the new one until it re-fetched. Rotation is therefore deliberate — add a version,
    publish the new public key, wait for verifiers to pick it up, then disable the old version.
    Terraform expressing it as a period would imply an automation that does not exist.
  EOT

  type = map(object({
    # RSA_SIGN_PKCS1_2048_SHA256 verifies with any JWT library on any runtime and is the
    # conservative default. EC_SIGN_P256_SHA256 produces signatures a third the size, which
    # matters when the token rides on every internal call — but check the verifying libraries
    # first, since ECDSA support is less uniform than RSA.
    algorithm = optional(string, "RSA_SIGN_PKCS1_2048_SHA256")

    # HSM by default here, unlike the symmetric keys. The cost difference is small and this is
    # the key whose compromise has no other control behind it.
    protection_level = optional(string, "HSM")

    destroy_scheduled_duration_seconds = optional(number, 2592000)
    labels                             = optional(map(string), {})
  }))
  default = {}

  validation {
    condition = alltrue([
      for name, key in var.signing_keys :
      can(regex("^[a-z](?:[-a-z0-9]{0,61}[a-z0-9])?$", name)) &&
      contains([
        "RSA_SIGN_PKCS1_2048_SHA256",
        "RSA_SIGN_PKCS1_3072_SHA256",
        "RSA_SIGN_PKCS1_4096_SHA256",
        "RSA_SIGN_PSS_2048_SHA256",
        "RSA_SIGN_PSS_3072_SHA256",
        "RSA_SIGN_PSS_4096_SHA256",
        "EC_SIGN_P256_SHA256",
        "EC_SIGN_P384_SHA384",
      ], key.algorithm) &&
      contains(["SOFTWARE", "HSM"], key.protection_level) &&
      key.destroy_scheduled_duration_seconds >= 86400 &&
      key.destroy_scheduled_duration_seconds <= 10368000
    ])
    error_message = "signing_keys require valid names, a supported ASYMMETRIC_SIGN algorithm, SOFTWARE or HSM protection, and a 1-120 day destruction delay."
  }

  validation {
    condition     = length(var.keys) + length(var.signing_keys) > 0
    error_message = "This module creates a key ring; give it at least one key or signing key, or do not instantiate it."
  }

  validation {
    condition     = length(setintersection(keys(var.keys), keys(var.signing_keys))) == 0
    error_message = "A name cannot appear in both keys and signing_keys — they would collide on the same CryptoKey resource name."
  }
}

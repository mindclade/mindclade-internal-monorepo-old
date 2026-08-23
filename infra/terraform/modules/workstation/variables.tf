# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

variable "project_id" {
  description = "Project that owns the workstation instance, disk, and identity"
  type        = string

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{4,28}[a-z0-9]$", var.project_id))
    error_message = "project_id must be a valid GCP project identifier."
  }
}

variable "name" {
  description = "Workstation instance name; derived resources append a suffix"
  type        = string

  validation {
    condition     = can(regex("^[a-z](?:[-a-z0-9]{0,61}[a-z0-9])?$", var.name))
    error_message = "name must be a valid RFC1035 resource name."
  }

  validation {
    # `${name}-iap-ssh` and `${name}-data` must both stay inside RFC1035's 63 characters.
    condition     = length(var.name) <= 50
    error_message = "name must be at most 50 characters so derived resource names remain valid."
  }
}

variable "region" {
  description = "Region holding the persistent data disk's CMEK and the instance schedule"
  type        = string

  validation {
    condition     = can(regex("^[a-z]+(?:-[a-z0-9]+)+[0-9]$", var.region))
    error_message = "region must be a valid GCP region."
  }
}

variable "zone" {
  description = "Zone for the instance and its zonal persistent disk"
  type        = string

  validation {
    condition     = can(regex("^[a-z]+(?:-[a-z0-9]+)+[0-9]-[a-z]$", var.zone))
    error_message = "zone must be a valid GCP zone."
  }
}

variable "subnetwork" {
  description = "Fully qualified subnetwork hosting the workstation's only interface"
  type        = string

  validation {
    condition = can(regex("^projects/[^/]+/regions/[^/]+/subnetworks/[^/]+$", var.subnetwork)) || can(
      regex("^https://www\\.googleapis\\.com/compute/[^/]+/projects/[^/]+/regions/[^/]+/subnetworks/[^/]+$", var.subnetwork)
    )
    error_message = "subnetwork must be a fully qualified subnetwork path or self-link."
  }
}

variable "network" {
  description = "Network owning the IAP ingress rule; required when this module creates that rule"
  type        = string
  default     = null

  validation {
    condition = var.network == null || can(regex("^[a-z](?:[-a-z0-9]{0,61}[a-z0-9])?$", var.network)) || can(
      regex("^projects/[^/]+/global/networks/[^/]+$", var.network)
    )
    error_message = "network must be a network name or a fully qualified network path."
  }
}

variable "kms_key_name" {
  description = "Required CMEK protecting both the boot disk and the persistent data disk"
  type        = string

  validation {
    condition     = can(regex("^projects/[a-z][a-z0-9-]{4,28}[a-z0-9]/locations/[^/]+/keyRings/[A-Za-z0-9_-]+/cryptoKeys/[A-Za-z0-9_-]+$", var.kms_key_name))
    error_message = "kms_key_name must be a complete CryptoKey resource name."
  }
}

variable "service_account_id" {
  description = "Account id for the workstation's dedicated keyless identity"
  type        = string

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{4,28}[a-z0-9]$", var.service_account_id))
    error_message = "service_account_id must be a valid service account identifier."
  }
}

variable "operator_principals" {
  description = "Human principals permitted to open an IAP tunnel and log in"
  type        = set(string)

  validation {
    condition     = length(var.operator_principals) >= 1 && length(var.operator_principals) <= 8
    error_message = "operator_principals must contain between 1 and 8 principals."
  }

  validation {
    condition     = alltrue([for p in var.operator_principals : can(regex("^(user|group):[^[:space:]]+@[^[:space:]]+$", p))])
    error_message = "Each operator principal must be a user: or group: address."
  }

  validation {
    # The inverse of the cache modules' rule, and deliberately so. Those grant machine access and
    # forbid humans; IAP SSH is a human path, and a service account holding tunnel access is an
    # unattended door into a box that can reach the caches.
    condition = alltrue([
      for p in var.operator_principals :
      !can(regex("(?i)(allusers|allauthenticatedusers|^domain:|^serviceaccount:|\\*)", p))
    ])
    error_message = "operator_principals must not include public, domain, wildcard, or service-account principals."
  }
}

variable "machine_type" {
  description = "x86_64 machine type; Arm is forbidden by the repository's toolchain contract"
  type        = string
  default     = "c2d-standard-16"

  validation {
    condition = contains([
      "c2d-standard-16",
      "c2d-standard-32",
      "c2d-highmem-16",
      "n2-standard-16",
      "n2-standard-32",
      "n2-highmem-16",
      "t2d-standard-16",
      "t2d-standard-32",
    ], var.machine_type)
    error_message = "machine_type must be an approved x86_64 type. Arm types (c4a, t2a) are forbidden: the repository's .#gpu shell is x86_64-linux only and no aarch64 CUDA target exists, so an Arm workstation cannot enter the shell it exists to run. c3d types are excluded because the organization holds no C3D quota."
  }
}

variable "image" {
  description = "Boot image as project/family or a full image self-link"
  type        = string
  default     = "debian-cloud/debian-12"

  validation {
    condition = can(regex("^[a-z][a-z0-9-]*/[a-z][a-z0-9-]*$", var.image)) || can(
      regex("^projects/[^/]+/global/images/[^/]+$", var.image)
    )
    error_message = "image must be project/family or a fully qualified image path."
  }
}

# PROVISIONING SOURCES.
#
# The startup script fetches Debian packages and the Nix installer, and both defaults are public
# internet endpoints. This module targets an environment whose firewall denies egress by default at
# priority 65000 — only intra-VPC destinations, the restricted Google API VIP, and the metadata
# server are reachable — so there the defaults are not slow, they are blocked, and the instance
# boots reachable-but-unprovisioned.
#
# These inputs are the only place that can be fixed: var.metadata refuses the startup-script key,
# so no caller can substitute a script of its own. Each one defaults to null, which keeps today's
# public source and today's rendered script unchanged; setting one REPLACES that source rather than
# adding to it. None of them creates a mirror. README.md states what an estate must still stand up
# outside this module for a locked-down deployment to actually provision.
#
# Every URL here is rendered into instance metadata, which any principal holding
# compute.instances.get can read, and the Nix installer is executed as root. That is why the
# validations below are narrow rather than "must look like a URL": an input that accepts anything
# is how the installer eventually comes from a host nobody reviewed.

variable "apt_mirror_url" {
  description = "Internal Debian mirror base URL replacing the image's sources.list; null keeps the image default"
  type        = string
  default     = null

  validation {
    # https only, and a dotted DNS host only. Plain http puts the package set of a machine that can
    # reach the caches on an unauthenticated wire; a bare IP literal is a destination no reviewer
    # can tie to an owner and no DNS policy can redirect; userinfo, query, and fragment are refused
    # because a mirror base carrying a credential would be published in instance metadata.
    condition = var.apt_mirror_url == null || can(regex(
      "^https://[a-z0-9]([a-z0-9-]*[a-z0-9])?(\\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)*\\.[a-z]{2,}(:[0-9]{1,5})?(/[A-Za-z0-9._~/-]*)?$",
      var.apt_mirror_url
    ))
    error_message = "apt_mirror_url must be null or an https URL naming a dotted DNS host, with no userinfo, query string, or fragment. Plain http, a bare IP literal, and an embedded credential are refused: this value is rendered into instance metadata that every principal holding compute.instances.get can read."
  }
}

variable "apt_mirror_components" {
  description = "Debian components published by apt_mirror_url; null means main only"
  type        = list(string)
  default     = null

  validation {
    # Written verbatim into sources.list, so the set is closed. An unconstrained string here is a
    # repository line that reaches a root apt-get without anyone having read it.
    condition = var.apt_mirror_components == null ? true : (
      length(var.apt_mirror_components) >= 1 &&
      length(var.apt_mirror_components) <= 4 &&
      alltrue([
        for component in var.apt_mirror_components :
        contains(["main", "contrib", "non-free", "non-free-firmware"], component)
      ])
    )
    error_message = "apt_mirror_components must be null or between one and four of main, contrib, non-free, non-free-firmware."
  }
}

variable "nix_installer_url" {
  description = "Internal Nix installer script URL replacing nixos.org; null keeps the public installer"
  type        = string
  default     = null

  validation {
    # The same shape as apt_mirror_url, and for a stronger reason: whatever this URL serves is run
    # as root by the startup script. nix_installer_sha256 exists to pin what it serves.
    condition = var.nix_installer_url == null || can(regex(
      "^https://[a-z0-9]([a-z0-9-]*[a-z0-9])?(\\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)*\\.[a-z]{2,}(:[0-9]{1,5})?(/[A-Za-z0-9._~/-]*)?$",
      var.nix_installer_url
    ))
    error_message = "nix_installer_url must be null or an https URL naming a dotted DNS host, with no userinfo, query string, or fragment. What this URL serves is executed as root, so plain http, a bare IP literal, and an embedded credential are refused."
  }
}

variable "nix_installer_sha256" {
  description = "SHA-256 pin for the fetched Nix installer; a mismatch refuses to run it"
  type        = string
  default     = null

  validation {
    condition     = var.nix_installer_sha256 == null || can(regex("^[a-f0-9]{64}$", var.nix_installer_sha256))
    error_message = "nix_installer_sha256 must be null or 64 lowercase hexadecimal characters."
  }
}

variable "nix_substituter_uri" {
  description = "Reviewed Nix substituter for the guest; null because no such service exists yet"
  type        = string
  default     = null

  validation {
    # This is the hook a reviewed substituter service plugs into. It is NOT where the estate's Nix
    # cache bucket goes: nix_binary_cache exports substituter_uri = null and
    # client_activation_contract.enabled = false with reason
    # raw-private-gcs-is-not-a-nix-substituter, because raw private GCS does not speak the
    # authenticated Nix cache protocol. Pointing this at that bucket contradicts a module contract.
    condition = var.nix_substituter_uri == null || can(regex(
      "^https://[a-z0-9]([a-z0-9-]*[a-z0-9])?(\\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)*\\.[a-z]{2,}(:[0-9]{1,5})?(/[A-Za-z0-9._~/-]*)?$",
      var.nix_substituter_uri
    ))
    error_message = "nix_substituter_uri must be null or an https URL naming a dotted DNS host, with no userinfo, query string, or fragment. It names a reviewed substituter service; a raw private Cloud Storage bucket is not one."
  }
}

variable "nix_substituter_trusted_public_key" {
  description = "Ed25519 key trusted for nix_substituter_uri, as name:base64; required with it"
  type        = string
  default     = null

  validation {
    # Nix's own trusted-public-keys format. require-sigs is never relaxed by this module, so this
    # key is the whole reason a path served by the substituter is acceptable to the guest.
    condition = var.nix_substituter_trusted_public_key == null || can(
      regex("^[A-Za-z0-9._-]+:[A-Za-z0-9+/]{43}=$", var.nix_substituter_trusted_public_key)
    )
    error_message = "nix_substituter_trusted_public_key must be null or a Nix trusted-public-keys entry of the form name:base64, where base64 encodes 32 ed25519 bytes."
  }
}

variable "boot_disk_size_gb" {
  description = "Boot disk size; the Nix store and Bazel cache live on the data disk, not here"
  type        = number
  default     = 200

  validation {
    condition     = floor(var.boot_disk_size_gb) == var.boot_disk_size_gb && var.boot_disk_size_gb >= 100 && var.boot_disk_size_gb <= 500
    error_message = "boot_disk_size_gb must be a whole number between 100 and 500."
  }
}

variable "data_disk_size_gb" {
  description = "Persistent data disk carrying /nix and the Bazel disk cache"
  type        = number
  default     = 500

  validation {
    condition     = floor(var.data_disk_size_gb) == var.data_disk_size_gb && var.data_disk_size_gb >= 200 && var.data_disk_size_gb <= 4000
    error_message = "data_disk_size_gb must be a whole number between 200 and 4000. The measured reference /nix/store is 46 GB; the default leaves headroom for the Bazel disk cache and Rust target directories."
  }
}

variable "disk_type" {
  description = "Disk type for both the boot and data disks"
  type        = string
  default     = "pd-balanced"

  validation {
    condition     = contains(["pd-balanced", "pd-ssd"], var.disk_type)
    error_message = "disk_type must be pd-balanced or pd-ssd."
  }
}

variable "local_ssd_count" {
  description = "Ephemeral NVMe scratch disks for the Bazel cache only; never for /nix"
  type        = number
  default     = 0

  validation {
    condition     = floor(var.local_ssd_count) == var.local_ssd_count && var.local_ssd_count >= 0 && var.local_ssd_count <= 8
    error_message = "local_ssd_count must be a whole number between 0 and 8."
  }
}

variable "idle_shutdown_minutes" {
  description = "Bounded idle period after which the guest powers itself off"
  type        = number
  default     = 60

  validation {
    condition     = floor(var.idle_shutdown_minutes) == var.idle_shutdown_minutes && var.idle_shutdown_minutes >= 15 && var.idle_shutdown_minutes <= 480
    error_message = "idle_shutdown_minutes must be a whole number between 15 and 480."
  }
}

variable "idle_check_interval_seconds" {
  description = "Bounded polling interval for the idle check timer"
  type        = number
  default     = 300

  validation {
    condition     = floor(var.idle_check_interval_seconds) == var.idle_check_interval_seconds && var.idle_check_interval_seconds >= 60 && var.idle_check_interval_seconds <= 1800
    error_message = "idle_check_interval_seconds must be a whole number between 60 and 1800."
  }
}

variable "idle_load_threshold" {
  description = "One-minute load average below which the workstation counts as idle"
  type        = number
  default     = 0.5

  validation {
    condition     = var.idle_load_threshold > 0 && var.idle_load_threshold <= 4
    error_message = "idle_load_threshold must be greater than 0 and at most 4."
  }
}

variable "daily_stop_schedule" {
  description = "Optional cron stop schedule; there is deliberately no start schedule"
  type        = string
  default     = "0 3 * * *"

  validation {
    condition     = var.daily_stop_schedule == null || can(regex("^([0-9*,/-]+ ){4}[0-9*,/-]+$", var.daily_stop_schedule))
    error_message = "daily_stop_schedule must be null or a five-field cron expression."
  }
}

variable "schedule_timezone" {
  description = "IANA time zone for the stop schedule"
  type        = string
  default     = "Etc/UTC"

  validation {
    condition     = length(var.schedule_timezone) > 0 && length(var.schedule_timezone) <= 64
    error_message = "schedule_timezone must be a non-empty time zone of at most 64 characters."
  }
}

variable "create_iap_ssh_firewall_rule" {
  description = "Create the IAP ingress rule here; set false when firewall rules are centralized"
  type        = bool
  default     = true
}

variable "network_tag" {
  description = "Network tag binding the IAP ingress rule to this instance"
  type        = string
  default     = null

  validation {
    condition     = var.network_tag == null || can(regex("^[a-z]([-a-z0-9]{0,61}[a-z0-9])?$", var.network_tag))
    error_message = "network_tag must be a valid network tag."
  }
}

variable "os_login_role" {
  description = "OS Login role granted to operators; osAdminLogin grants sudo"
  type        = string
  default     = "roles/compute.osLogin"

  validation {
    condition     = contains(["roles/compute.osLogin", "roles/compute.osAdminLogin"], var.os_login_role)
    error_message = "os_login_role must be roles/compute.osLogin or roles/compute.osAdminLogin. The startup script already performs every root-requiring boot action, so osAdminLogin should be a deliberate choice rather than a default."
  }
}

variable "extra_project_roles" {
  description = "Additional project roles for the workstation identity; signing and release authority is refused"
  type        = set(string)
  default     = []

  validation {
    condition     = length(var.extra_project_roles) <= 8
    error_message = "extra_project_roles must contain at most 8 roles."
  }

  validation {
    condition     = alltrue([for r in var.extra_project_roles : can(regex("^roles/[A-Za-z0-9_.]+$", r))])
    error_message = "extra_project_roles must be predefined roles."
  }

  validation {
    condition = alltrue([
      for r in var.extra_project_roles :
      !contains(["roles/owner", "roles/editor", "roles/viewer"], r) && !endswith(lower(r), ".admin")
    ])
    error_message = "extra_project_roles must not include basic or admin roles."
  }

  validation {
    # A developer workstation is never a release authority. Each of these would let the box sign,
    # attest, publish, or mint credentials, which is exactly the boundary the ARC runner-group
    # split exists to hold.
    condition = alltrue([
      for r in var.extra_project_roles :
      !contains([
        "roles/cloudkms.signer",
        "roles/cloudkms.signerVerifier",
        "roles/cloudkms.cryptoOperator",
        "roles/cloudkms.cryptoKeyEncrypterDecrypter",
        "roles/binaryauthorization.attestorsVerifier",
        "roles/containeranalysis.notes.attacher",
        "roles/containeranalysis.occurrences.editor",
        "roles/artifactregistry.writer",
        "roles/artifactregistry.repoAdmin",
        "roles/iam.serviceAccountTokenCreator",
        "roles/iam.serviceAccountKeyAdmin",
        "roles/compute.instanceAdmin.v1",
      ], r)
    ])
    error_message = "extra_project_roles must not grant signing, attestation, publication, or credential-minting authority."
  }
}

variable "metadata" {
  description = "Additional instance metadata; module-owned keys are refused"
  type        = map(string)
  default     = {}

  validation {
    condition     = length(var.metadata) <= 16
    error_message = "metadata must contain at most 16 entries."
  }

  validation {
    condition = length(setintersection(keys(var.metadata), [
      "enable-oslogin",
      "block-project-ssh-keys",
      "serial-port-enable",
      "startup-script",
      "shutdown-script",
      "ssh-keys",
    ])) == 0
    error_message = "metadata must not override module-owned keys."
  }
}

variable "labels" {
  description = "Additional resource labels merged under the module's baseline labels"
  type        = map(string)
  default     = {}

  validation {
    condition     = length(var.labels) <= 58
    error_message = "labels must contain at most 58 entries."
  }

  validation {
    condition = alltrue([
      for k, v in var.labels :
      can(regex("^[a-z][a-z0-9_-]{0,62}$", k)) && can(regex("^$|^[a-z0-9][a-z0-9_-]{0,62}$", v))
    ])
    error_message = "labels must use valid GCP label keys and values."
  }
}

variable "environment" {
  description = "Environment governance label"
  type        = string

  validation {
    condition     = can(regex("^[a-z0-9][a-z0-9_-]{0,62}$", var.environment))
    error_message = "environment must be a valid non-empty GCP label value."
  }
}

variable "owner" {
  description = "Accountable team governance label"
  type        = string

  validation {
    condition     = can(regex("^[a-z0-9][a-z0-9_-]{0,62}$", var.owner))
    error_message = "owner must be a valid non-empty GCP label value."
  }
}

variable "data_classification" {
  description = "Governance classification; public is forbidden"
  type        = string
  default     = "internal"

  validation {
    condition     = contains(["internal", "confidential", "restricted"], var.data_classification)
    error_message = "data_classification must be internal, confidential, or restricted."
  }
}

variable "nix_cache_bucket_name" {
  description = "Nix binary cache bucket the workstation identity may read"
  type        = string

  validation {
    condition     = can(regex("^[a-z0-9][a-z0-9._-]{1,61}[a-z0-9]$", var.nix_cache_bucket_name))
    error_message = "nix_cache_bucket_name must be a valid Cloud Storage bucket name."
  }
}

variable "bazel_cache_bucket_name" {
  description = "Bazel remote cache bucket the workstation identity may read and write"
  type        = string

  validation {
    condition     = can(regex("^[a-z0-9][a-z0-9._-]{1,61}[a-z0-9]$", var.bazel_cache_bucket_name))
    error_message = "bazel_cache_bucket_name must be a valid Cloud Storage bucket name."
  }
}

variable "deletion_protection" {
  description = "Guard against accidental instance deletion"
  type        = bool
  default     = true
}

variable "allow_stopping_for_update" {
  description = "Permit Terraform to stop the instance when an update requires it"
  type        = bool
  default     = true
}

variable "project_id" {
  description = "Google Cloud project that owns the VPC network and the reserved peering range"
  type        = string

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{4,28}[a-z0-9]$", var.project_id))
    error_message = "project_id must be a valid Google Cloud project ID."
  }
}

variable "network_id" {
  description = "Fully qualified resource ID of the VPC network to peer with Google managed services"
  type        = string

  validation {
    condition = can(
      regex(
        "^projects/[a-z][a-z0-9-]{4,28}[a-z0-9]/global/networks/[a-z](?:[-a-z0-9]{0,61}[a-z0-9])?$",
        var.network_id,
      )
    )
    error_message = "network_id must be a canonical projects/<project>/global/networks/<name> resource ID."
  }
}

variable "reserved_range_name" {
  description = "Name of the reserved internal range allocated to Google managed services"
  type        = string

  validation {
    condition     = can(regex("^[a-z](?:[-a-z0-9]{0,61}[a-z0-9])?$", var.reserved_range_name))
    error_message = "reserved_range_name must be a valid 1-63 character RFC1035 name."
  }
}

variable "reserved_range_description" {
  description = "Purpose and ownership of the reserved managed-services range"
  type        = string
  default     = "Reserved range for Google managed services reached over private services access."

  validation {
    condition     = length(trimspace(var.reserved_range_description)) > 0 && length(var.reserved_range_description) <= 256
    error_message = "reserved_range_description must contain 1-256 characters."
  }
}

variable "address" {
  description = "First IPv4 address of the reserved range; leave empty to let Google choose a free block"
  type        = string
  default     = ""

  validation {
    condition = (
      var.address == "" ||
      can(regex("^(?:[0-9]{1,3}\\.){3}[0-9]{1,3}$", var.address)) &&
      can(cidrnetmask("${var.address}/32"))
    )
    error_message = "address must be empty or a valid IPv4 address."
  }
}

variable "prefix_length" {
  description = "Prefix length of the reserved range; managed services carve service subnets out of this block"
  type        = number
  default     = 20

  validation {
    condition     = var.prefix_length >= 16 && var.prefix_length <= 24 && floor(var.prefix_length) == var.prefix_length
    error_message = "prefix_length must be a whole number from 16 through 24."
  }
}

variable "export_custom_routes" {
  description = "Export custom routes to the service producer network, for example for cross-region replica reachability over hybrid connectivity"
  type        = bool
  default     = false
}

variable "import_custom_routes" {
  description = "Import custom routes advertised by the service producer network"
  type        = bool
  default     = false
}

variable "labels" {
  description = "Labels applied to the reserved range"
  type        = map(string)
  default     = {}

  validation {
    condition = alltrue([
      for key, value in var.labels :
      can(regex("^[a-z][a-z0-9_-]{0,62}$", key)) && can(regex("^[a-z0-9_-]{0,63}$", value))
    ])
    error_message = "labels must use lowercase keys and values of at most 63 characters."
  }
}

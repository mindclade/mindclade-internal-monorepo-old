variable "networks" {
  description = <<-EOT
    VPC networks keyed by a stable name — in this estate, the environment.

    A map rather than a single network because `3-networks/` sits above the environment level:
    one unit builds development, staging, and production so that the subnet layout cannot
    drift between them. Every output here is therefore keyed the same way, and a consumer in
    `5-workloads` indexes it with its own environment.

    Splitting this into one module call per environment would mean three copies of the same
    peering, NAT, and subnet logic kept in step by hand, and the failure when they drift is an
    address collision discovered the day someone peers two of them.
  EOT

  type = map(object({
    project_id   = string
    network_name = string
    description  = optional(string, "Mindclade Shared VPC managed by Terraform.")

    routing_mode                      = optional(string, "REGIONAL")
    mtu                               = optional(number, 1460)
    firewall_policy_enforcement_order = optional(string, "AFTER_CLASSIC_FIREWALL")

    # Which subnet GKE's nodes take addresses from. Named rather than inferred: several
    # subnets in this network are not node subnets, and picking one by position would make
    # adding a subnet silently move the cluster.
    #
    # Its secondary ranges are matched by suffix — `*-pods` and `*-services` — to produce the
    # `pods_range_names` and `services_range_names` outputs the GKE units consume. The suffix
    # is a convention, so it is validated below rather than left to fail at cluster creation
    # with an error naming the range and not this file.
    primary_subnet_key = optional(string, "nodes")

    # The default 0.0.0.0/0 route is deleted at creation and replaced by this one, so that
    # egress has exactly one path and it appears in flow logs with a stable source address.
    # Public Cloud NAT requires it — there is a precondition below that says so.
    create_default_internet_route   = optional(bool, true)
    default_internet_route_priority = optional(number, 1000)

    subnets = map(object({
      region        = string
      ip_cidr_range = string
      description   = optional(string, "Mindclade private subnet managed by Terraform.")

      # PRIVATE for workload subnets. REGIONAL_MANAGED_PROXY is the proxy-only subnet a
      # regional internal Application Load Balancer (gke-l7-rilb) reserves its Envoy fleet
      # from — it holds proxies, never addresses, so nothing can be allocated from it and a
      # Gateway VIP must come from a PRIVATE subnet instead.
      purpose = optional(string, "PRIVATE")

      # ACTIVE or BACKUP, and only meaningful for REGIONAL_MANAGED_PROXY. Exactly one ACTIVE
      # proxy-only subnet may exist per region per network; a second ACTIVE one is rejected by
      # the API with an error that names neither subnet.
      role = optional(string)

      secondary_ip_ranges = optional(map(string), {})

      flow_logs = optional(object({
        enabled              = optional(bool, true)
        aggregation_interval = optional(string, "INTERVAL_5_MIN")
        sampling             = optional(number, 0.5)
        filter               = optional(string, "true")
      }), {})
    }))

    nat_gateways = optional(map(object({
      region                 = string
      router_name            = string
      nat_name               = string
      subnet_keys            = set(string)
      nat_ip_allocate_option = optional(string, "AUTO_ONLY")
      nat_ips                = optional(list(string), [])
      min_ports_per_vm       = optional(number, 64)
      log_filter             = optional(string, "ERRORS_ONLY")
    })), {})
  }))

  validation {
    condition = length(var.networks) > 0 && alltrue([
      for net_key, net in var.networks :
      can(regex("^[a-z](?:[-a-z0-9]{0,61}[a-z0-9])?$", net.network_name)) &&
      contains(["REGIONAL", "GLOBAL"], net.routing_mode) &&
      net.mtu >= 1300 && net.mtu <= 8896
    ])
    error_message = "Each network requires an RFC1035 name, a REGIONAL or GLOBAL routing mode, and an MTU between 1300 and 8896."
  }

  validation {
    condition = alltrue(flatten([
      for net_key, net in var.networks : [
        for name, subnet in net.subnets :
        can(regex("^[a-z](?:[-a-z0-9]{0,61}[a-z0-9])?$", name)) &&
        can(regex("^[a-z]+(?:-[a-z0-9]+)+[0-9]$", subnet.region)) &&
        can(cidrnetmask(subnet.ip_cidr_range)) &&
        alltrue([
          for range_name, cidr in subnet.secondary_ip_ranges :
          can(regex("^[a-z](?:[-a-z0-9]{0,61}[a-z0-9])?$", range_name)) &&
          can(cidrnetmask(cidr))
        ]) &&
        contains([
          "INTERVAL_5_SEC",
          "INTERVAL_30_SEC",
          "INTERVAL_1_MIN",
          "INTERVAL_5_MIN",
          "INTERVAL_10_MIN",
          "INTERVAL_15_MIN",
        ], subnet.flow_logs.aggregation_interval) &&
        subnet.flow_logs.sampling >= 0 && subnet.flow_logs.sampling <= 1 &&
        length(trimspace(subnet.flow_logs.filter)) > 0
      ]
    ]))
    error_message = "subnets require valid names, regions, IPv4 CIDRs, secondary ranges, and VPC Flow Logs settings."
  }

  validation {
    condition = alltrue(flatten([
      for net_key, net in var.networks : [
        for name, subnet in net.subnets :
        contains(["PRIVATE", "REGIONAL_MANAGED_PROXY", "PRIVATE_SERVICE_CONNECT", "PEER_MIGRATION"], subnet.purpose)
      ]
    ]))
    error_message = "subnet purpose must be PRIVATE, REGIONAL_MANAGED_PROXY, PRIVATE_SERVICE_CONNECT, or PEER_MIGRATION."
  }

  # A proxy-only subnet is not a subnet workloads live in: it has no secondary ranges, takes
  # no flow logs, and cannot grant Private Google Access. Setting any of them is accepted by
  # Terraform and rejected by the API, with an error that names the field and not the reason.
  validation {
    condition = alltrue(flatten([
      for net_key, net in var.networks : [
        for name, subnet in net.subnets :
        length(subnet.secondary_ip_ranges) == 0 && !subnet.flow_logs.enabled
        if subnet.purpose == "REGIONAL_MANAGED_PROXY"
      ]
    ]))
    error_message = "A REGIONAL_MANAGED_PROXY subnet holds proxies, not workloads: it can carry no secondary ranges and no flow logs. Set flow_logs = { enabled = false } and remove secondary_ip_ranges."
  }

  # `role` is meaningful only for a proxy-only subnet, and there it is required — a
  # REGIONAL_MANAGED_PROXY subnet without one is created in an indeterminate state that the
  # load balancer will not use.
  validation {
    condition = alltrue(flatten([
      for net_key, net in var.networks : [
        for name, subnet in net.subnets :
        subnet.purpose == "REGIONAL_MANAGED_PROXY"
        ? contains(["ACTIVE", "BACKUP"], coalesce(subnet.role, "unset"))
        : subnet.role == null
      ]
    ]))
    error_message = "role must be ACTIVE or BACKUP on a REGIONAL_MANAGED_PROXY subnet, and unset on every other purpose."
  }

  # Exactly one ACTIVE proxy-only subnet per region per network. A second one is rejected by
  # the API in a way that names neither subnet, so it is caught here where both are visible.
  validation {
    condition = alltrue([
      for net_key, net in var.networks :
      length([
        for name, subnet in net.subnets : subnet.region
        if subnet.purpose == "REGIONAL_MANAGED_PROXY" && subnet.role == "ACTIVE"
        ]) == length(distinct([
          for name, subnet in net.subnets : subnet.region
          if subnet.purpose == "REGIONAL_MANAGED_PROXY" && subnet.role == "ACTIVE"
      ]))
    ])
    error_message = "At most one ACTIVE REGIONAL_MANAGED_PROXY subnet may exist per region in a network."
  }

  # The primary subnet must exist, be a workload subnet, and carry exactly one `-pods` and one
  # `-services` secondary range. Every one of those is assumed by the GKE units downstream,
  # and each failure surfaces there rather than here — as a cluster that will not create, with
  # an error naming a range name and nothing pointing back at the network definition.
  validation {
    condition = alltrue([
      for net_key, net in var.networks :
      try(net.subnets[net.primary_subnet_key].purpose, "") == "PRIVATE" &&
      length([
        for name, _ in net.subnets[net.primary_subnet_key].secondary_ip_ranges : name
        if endswith(name, "-pods")
      ]) == 1 &&
      length([
        for name, _ in net.subnets[net.primary_subnet_key].secondary_ip_ranges : name
        if endswith(name, "-services")
      ]) == 1
    ])
    error_message = "Each network's primary_subnet_key must name an existing PRIVATE subnet carrying exactly one secondary range ending in \"-pods\" and one ending in \"-services\"."
  }

  validation {
    condition = alltrue(flatten([
      for net_key, net in var.networks : [
        for gateway_key, gateway in net.nat_gateways :
        can(regex("^[a-z](?:[-a-z0-9]{0,61}[a-z0-9])?$", gateway.router_name)) &&
        can(regex("^[a-z](?:[-a-z0-9]{0,61}[a-z0-9])?$", gateway.nat_name)) &&
        can(regex("^[a-z]+(?:-[a-z0-9]+)+[0-9]$", gateway.region)) &&
        length(gateway.subnet_keys) > 0 &&
        contains(["AUTO_ONLY", "MANUAL_ONLY"], gateway.nat_ip_allocate_option) &&
        (gateway.nat_ip_allocate_option == "MANUAL_ONLY" ? length(gateway.nat_ips) > 0 : length(gateway.nat_ips) == 0) &&
        gateway.min_ports_per_vm >= 32 &&
        contains(["ERRORS_ONLY", "TRANSLATIONS_ONLY", "ALL"], gateway.log_filter)
      ]
    ]))
    error_message = "nat_gateways require RFC1035 names, a region, at least one subnet key, a consistent IP allocation mode, a port floor of at least 32, and a supported log filter."
  }
}

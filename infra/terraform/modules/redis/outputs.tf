# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

output "instance" {
  description = "Non-secret connection metadata; use TLS and retrieve auth through a protected runtime path"
  value = {
    id            = google_redis_instance.this.id
    host          = google_redis_instance.this.host
    port          = google_redis_instance.this.port
    read_endpoint = google_redis_instance.this.read_endpoint
    region        = google_redis_instance.this.region
  }
}

output "server_ca_certs" {
  description = "Server CA certificates for TLS client trust configuration"
  value       = google_redis_instance.this.server_ca_certs
}

# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

output "primary" {
  description = "Primary instance connection metadata"
  value = {
    id                 = google_sql_database_instance.primary.id
    name               = google_sql_database_instance.primary.name
    connection_name    = google_sql_database_instance.primary.connection_name
    private_ip_address = google_sql_database_instance.primary.private_ip_address
    service_account    = google_sql_database_instance.primary.service_account_email_address
  }
}

output "databases" {
  description = "Created database names"
  value       = { for name, database in google_sql_database.this : name => database.self_link }
}

output "read_replicas" {
  description = "Replica connection metadata"
  value = {
    for name, replica in google_sql_database_instance.replica : name => {
      connection_name    = replica.connection_name
      private_ip_address = replica.private_ip_address
      region             = replica.region
    }
  }
}

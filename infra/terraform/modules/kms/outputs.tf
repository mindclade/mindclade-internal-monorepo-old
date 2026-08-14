output "key_ring_id" {
  description = "Fully qualified Cloud KMS key-ring resource ID"
  value       = google_kms_key_ring.this.id
}

output "key_ring_name" {
  description = "Cloud KMS key-ring name"
  value       = google_kms_key_ring.this.name
}

output "crypto_key_ids" {
  description = "Fully qualified CryptoKey resource IDs keyed by key name"
  value       = { for name, key in google_kms_crypto_key.this : name => key.id }
}

output "crypto_key_names" {
  description = "CryptoKey names keyed by the stable input name"
  value       = { for name, key in google_kms_crypto_key.this : name => key.name }
}

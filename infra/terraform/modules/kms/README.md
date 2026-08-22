# GCP Cloud KMS baseline

This module creates one immutable-location key ring and normally one or more
symmetric or asymmetric keys. Symmetric keys rotate every 90 days by default,
keep versions scheduled for destruction for 30 days, create an initial key
version, and use software protection unless a caller explicitly selects Cloud
HSM.

```hcl
module "kms" {
  source = "../../modules/kms"

  project_id    = "mindclade-security-prod"
  location      = "us-central1"
  key_ring_name = "application-data"
  keys = {
    primary = {
      protection_level = "HSM"
    }
  }
}
```

Set `ring_only = true` only when this state is the single protected owner of a
key ring and separately governed states own disjoint keys inside it. In that
mode both `keys` and `signing_keys` must be empty. With ring-only mode disabled,
at least one key remains mandatory, so an accidentally empty ordinary KMS state
still fails closed. Every key must have exactly one state owner.
Moving an existing key or key ring between ownership modes requires a
separately reviewed state-migration plan; this module does not perform or
authorize that move.

Cloud KMS key rings cannot be deleted through the API. CryptoKeys and key rings
also use Terraform lifecycle protection, and CryptoKeys use provider-side
deletion prevention. A CryptoKey's location, protection level, and destruction
delay are consequential immutable choices; changes can require replacement and
will be blocked until safeguards are deliberately reviewed. Verify Cloud HSM
availability, quotas, latency, data residency, organization policies, and
dependent-service recovery before rollout.

The optional `encrypter_decrypters` contract creates additive, key-scoped IAM member grants in
the same state that owns each symmetric key. Callers must name an existing key in `keys` and an
exact non-public principal; authoritative bindings and project-wide grants are intentionally not
supported. API enablement, service-agent provisioning, key-version disable/destroy procedures,
and application re-encryption remain separate operational boundaries. This module is a repository
baseline, not evidence that keys or grants are deployed or production-qualified.

<!-- BEGIN_TF_DOCS -->
## Requirements

| Name | Version |
| ---- | ------- |
| <a name="requirement_terraform"></a> [terraform](#requirement\_terraform) | >= 1.9.0, < 2.0.0 |
| <a name="requirement_google"></a> [google](#requirement\_google) | >= 7.41.0, < 8.0.0 |

## Providers

| Name | Version |
| ---- | ------- |
| <a name="provider_google"></a> [google](#provider\_google) | >= 7.41.0, < 8.0.0 |

## Inputs

| Name | Description | Type | Default | Required |
| ---- | ----------- | ---- | ------- | :------: |
| <a name="input_encrypter_decrypters"></a> [encrypter\_decrypters](#input\_encrypter\_decrypters) | Additive CryptoKey encrypter/decrypter members keyed by symmetric key name; service agents remain declared by the owning live state | `map(set(string))` | `{}` | no |
| <a name="input_key_ring_name"></a> [key\_ring\_name](#input\_key\_ring\_name) | Stable name for the Cloud KMS key ring | `string` | n/a | yes |
| <a name="input_keys"></a> [keys](#input\_keys) | Symmetric encryption keys keyed by stable CryptoKey name.<br/><br/>For ASYMMETRIC SIGNING keys — where the private key must never leave KMS — see<br/>`signing_keys` below. They are separate variables rather than one with a purpose field<br/>because almost nothing about them is shared: a signing key cannot carry an automatic<br/>rotation period, and treating them uniformly means a validation that permits an<br/>invalid combination on one of the two. | <pre>map(object({<br/>    rotation_period_seconds            = optional(number, 7776000)<br/>    destroy_scheduled_duration_seconds = optional(number, 2592000)<br/>    protection_level                   = optional(string, "SOFTWARE")<br/>    labels                             = optional(map(string), {})<br/>  }))</pre> | `{}` | no |
| <a name="input_labels"></a> [labels](#input\_labels) | Non-sensitive labels merged into every CryptoKey | `map(string)` | `{}` | no |
| <a name="input_location"></a> [location](#input\_location) | Immutable Cloud KMS location for the key ring | `string` | n/a | yes |
| <a name="input_project_id"></a> [project\_id](#input\_project\_id) | Google Cloud project that owns the key ring | `string` | n/a | yes |
| <a name="input_ring_only"></a> [ring\_only](#input\_ring\_only) | Create only the protected key ring; separately governed states must own disjoint keys in that ring. | `bool` | `false` | no |
| <a name="input_signing_keys"></a> [signing\_keys](#input\_signing\_keys) | Asymmetric signing keys keyed by stable CryptoKey name.<br/><br/>The distinguishing property is that the PRIVATE KEY NEVER LEAVES CLOUD KMS. Signing is an<br/>API call; the caller holds no key material, so a compromised pod cannot exfiltrate one.<br/>Verifiers fetch the public half and check signatures locally, with no call to KMS at all.<br/><br/>That is what makes this the right shape for the highest-blast-radius credential in the<br/>estate — the token-signing key. Every other secret here is bounded by something else: a<br/>session key by the IAP assertion that must accompany it, a DNS credential by zone scope, a<br/>deploy key by being read-only. A leaked signing key is bounded by nothing, because it mints<br/>tokens for any principal against any audience. So it is the one that must not exist outside<br/>KMS in the first place.<br/><br/>NO ROTATION PERIOD. Cloud KMS cannot automatically rotate an asymmetric key: rotation would<br/>create a new version, and every verifier holding the old public key would reject signatures<br/>made with the new one until it re-fetched. Rotation is therefore deliberate — add a version,<br/>publish the new public key, wait for verifiers to pick it up, then disable the old version.<br/>Terraform expressing it as a period would imply an automation that does not exist. | <pre>map(object({<br/>    # RSA_SIGN_PKCS1_2048_SHA256 verifies with any JWT library on any runtime and is the<br/>    # conservative default. EC_SIGN_P256_SHA256 produces signatures a third the size, which<br/>    # matters when the token rides on every internal call — but check the verifying libraries<br/>    # first, since ECDSA support is less uniform than RSA.<br/>    algorithm = optional(string, "RSA_SIGN_PKCS1_2048_SHA256")<br/><br/>    # HSM by default here, unlike the symmetric keys. The cost difference is small and this is<br/>    # the key whose compromise has no other control behind it.<br/>    protection_level = optional(string, "HSM")<br/><br/>    destroy_scheduled_duration_seconds = optional(number, 2592000)<br/>    labels                             = optional(map(string), {})<br/>  }))</pre> | `{}` | no |

## Outputs

| Name | Description |
| ---- | ----------- |
| <a name="output_crypto_key_ids"></a> [crypto\_key\_ids](#output\_crypto\_key\_ids) | Fully qualified CryptoKey resource IDs keyed by key name |
| <a name="output_crypto_key_names"></a> [crypto\_key\_names](#output\_crypto\_key\_names) | CryptoKey names keyed by the stable input name |
| <a name="output_encrypter_decrypter_grants"></a> [encrypter\_decrypter\_grants](#output\_encrypter\_decrypter\_grants) | Additive symmetric-key grants owned by this KMS state. |
| <a name="output_key_ring_id"></a> [key\_ring\_id](#output\_key\_ring\_id) | Fully qualified Cloud KMS key-ring resource ID |
| <a name="output_key_ring_name"></a> [key\_ring\_name](#output\_key\_ring\_name) | Cloud KMS key-ring name |
| <a name="output_ring_only"></a> [ring\_only](#output\_ring\_only) | Whether this state intentionally owns only the protected key ring. |
| <a name="output_signing_key_ids"></a> [signing\_key\_ids](#output\_signing\_key\_ids) | Asymmetric signing CryptoKey ids by name. The BFF signs by calling KMS with one of these; the private half never leaves. |
| <a name="output_signing_key_names"></a> [signing\_key\_names](#output\_signing\_key\_names) | Asymmetric signing CryptoKey short names. |
<!-- END_TF_DOCS -->

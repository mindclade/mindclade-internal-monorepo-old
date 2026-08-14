# GCP Cloud KMS baseline

This module creates one immutable-location key ring and one or more symmetric
encryption keys. Keys rotate every 90 days by default, keep versions scheduled
for destruction for 30 days, create an initial key version, and use software
protection unless a caller explicitly selects Cloud HSM.

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

Cloud KMS key rings cannot be deleted through the API. CryptoKeys and key rings
also use Terraform lifecycle protection, and CryptoKeys use provider-side
deletion prevention. A CryptoKey's location, protection level, and destruction
delay are consequential immutable choices; changes can require replacement and
will be blocked until safeguards are deliberately reviewed. Verify Cloud HSM
availability, quotas, latency, data residency, organization policies, and
dependent-service recovery before rollout.

IAM bindings, API enablement, service-agent permissions, key-version disable/
destroy procedures, and application re-encryption are separate operational
boundaries. This module is a repository baseline, not evidence that keys are
deployed or production-qualified.

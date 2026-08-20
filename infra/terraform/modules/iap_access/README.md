# IAP access module

This module grants `roles/iap.httpsResourceAccessor` to Google Groups on named
IAP-enabled Compute backend services. Grants use additive IAM member resources so
the module cannot replace unrelated accessors. Individual users, pre-prefixed
members, `allUsers`, and `allAuthenticatedUsers` are rejected.

```hcl
module "iap_access" {
  source = "../../modules/iap_access"

  project_id = "mindclade-production"
  backend_services = {
    studio = "k8s2-um-studio-abc123"
  }
  accessor_groups = ["studio-users@mindclade.com"]
}
```

The caller supplies actual Compute backend-service names after the GKE Gateway has
programmed them; Kubernetes Service names are not interchangeable. This module does
not enable IAP, configure OAuth, create load balancers, manage group membership, or
set application authorization. Removing a user from the group is the routine
revocation path; validate propagation/session revocation in a connected environment.

## Upgrade note

This module previously used one authoritative
`google_iap_web_backend_service_iam_binding.accessor` instance per backend. It now
uses additive `google_iap_web_backend_service_iam_member.accessor` instances per
backend and group. Existing callers must review the plan and migrate or remove the
old binding addresses under an approved state-change procedure; applying both
models concurrently can cause IAM churn. Do not run an automatic upgrade against a
production state.

Before rollout, prove an approved group member succeeds and an external Google
account fails, review effective inherited IAM, verify audit logging, and exercise the
offboarding path. Mock tests validate only the Terraform contract.

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
| <a name="input_accessor_groups"></a> [accessor\_groups](#input\_accessor\_groups) | Google groups permitted to complete an IAP sign-in.<br/><br/>GROUPS ONLY. A binding naming an individual outlives their employment; a group membership<br/>is removed by the same offboarding that removes everything else. | `set(string)` | n/a | yes |
| <a name="input_backend_services"></a> [backend\_services](#input\_backend\_services) | Compute backend service NAMES, keyed by the Kubernetes Service they came from.<br/><br/>GKE Gateway generates these names; they are not the Kubernetes Service names. Read them<br/>back after the Gateway is programmed:<br/><br/>  gcloud compute backend-services list --format='table(name, description)'<br/><br/>The description carries the originating Kubernetes Service, which is what makes the<br/>mapping legible rather than a guess at a hashed name.<br/><br/>Only the IAP-ENABLED backends belong here. studio-embed and go-vanity are deliberately<br/>absent — they carry no IAP policy, so a binding on them would grant a role against a<br/>control that is not there. | `map(string)` | n/a | yes |
| <a name="input_project_id"></a> [project\_id](#input\_project\_id) | Project holding the IAP-protected backend services. | `string` | n/a | yes |

## Outputs

| Name | Description |
| ---- | ----------- |
| <a name="output_accessor_members"></a> [accessor\_members](#output\_accessor\_members) | The IAM members granted roles/iap.httpsResourceAccessor.<br/><br/>Exported so a drift check can assert this set rather than re-deriving it — with a<br/>Google-managed OAuth client, an unexpected member here is the difference between an<br/>internal application and a public one. |
| <a name="output_bound_backend_services"></a> [bound\_backend\_services](#output\_bound\_backend\_services) | Backend services this module bound, keyed by Kubernetes Service name. |
<!-- END_TF_DOCS -->

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

# GCP project module

This module is the project-factory contract used below the organization and
folder foundation state. It creates one guarded project, disables default-network
creation, attaches billing, applies baseline labels, enables selected APIs, and
optionally adds a project budget and resource-manager tag bindings.

The module does not create a Google Cloud organization, folders, tag keys or tag
values, billing accounts, notification channels, or IAM grants. Those are
separate lifecycle and privilege boundaries.

Every project is protected by both `deletion_policy = "PREVENT"` and Terraform
`prevent_destroy`. API services remain enabled during destroy. A project must be
explicitly migrated out of this contract before an approved deletion workflow.
Project IDs and names follow the Resource Manager API grammar, including its
restricted ID strings. Budget email routing accepts at most five full Cloud
Monitoring email-channel resource names; the owning monitoring state must create
and validate those channels before this module is applied. Global project-ID
uniqueness can only be confirmed by the live Resource Manager API.

```hcl
module "application_project" {
  source = "../../modules/project"

  project_id         = "mindclade-development"
  project_name       = "Mindclade development"
  folder_id          = "folders/123456789012"
  billing_account_id = "ABCDEF-012345-6789AB"
  environment        = "development"
  owner              = "cloud-platform"

  activate_apis = [
    "logging.googleapis.com",
    "monitoring.googleapis.com",
  ]

  monthly_budget_usd = 500
  tag_value_names    = ["tagValues/234567890123"]
}
```

This is a reusable baseline, not a production-ready landing zone. Callers remain
responsible for IAM, network attachment, organization-policy inheritance,
centralized logging, alert routing, quota, and workload-specific controls.

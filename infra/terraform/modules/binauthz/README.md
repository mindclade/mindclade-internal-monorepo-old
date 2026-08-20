# `binauthz`

Binary Authorization: the point where a signature becomes a precondition for a pod running.

Signing an image and producing an SBOM changes nothing on its own — a cluster that will run
any image gains nothing from a signature existing somewhere. This module is the other half.

## The misconfiguration to know about

**`REQUIRE_ATTESTATION` with an empty attestor list admits every image**, while reading in the
console as the strictest setting available. Both the default rule and each namespace rule
reject that combination in variable validation, rather than leaving it to be discovered during
an incident.

Two more that fail quietly:

- **`cluster` must be exactly `<location>.<cluster_name>`.** Google matches the string
  literally, and a namespace rule whose cluster prefix is wrong applies to nothing while still
  appearing in the policy — which reads as coverage. Passing namespace rules with no `cluster`
  fails a precondition instead of silently doing nothing.
- **`global_policy_evaluation_mode = "DISABLE"`** stops the cluster starting `kube-proxy`, so
  the node never becomes ready. It presents as a networking problem and takes a long time to
  trace back here.

## Keys and who may sign

Each attestor gets a Container Analysis note and an asymmetric SIGN key in
`attestor_key_ring`, HSM-protected by default. The private half never leaves KMS: a pipeline
signs by calling KMS, so a compromised pipeline can mint attestations while it is compromised
but cannot take the key with it. The keys carry `prevent_destroy` — destroying one invalidates
every attestation ever made with it, including those on images currently running.

`project_number` is separate from `project_id` because the Binary Authorization service
agent is number-addressed. The module grants that service agent occurrence-viewer access on
each note so an attestor cannot exist while being unable to evaluate its attestations.

`attestor_signers` is who may *attest*, deliberately not who may *deploy*. Each signer gets
the three capabilities Google documents for the operation: attach an occurrence to that
specific Container Analysis note, create the occurrence in the attestation project, and use
the attestor's KMS key to sign. `roles/binaryauthorization.attestorsVerifier` is not a signer
role; it is granted separately through `attestor_verifiers` to the Binary Authorization
service agent in a deployer project. An identity that can both attest and deploy approves its
own artefacts, which is the control removed. An empty list is
meaningful and is not the same as omitting the entry: it declares that only a human granted
the role out of band may sign, which is how a human-review attestor stays un-automatable.

Upgrading from a release that used the old `attestor_signers` implementation changes IAM
resource addresses and corrects a semantic defect: the old code granted signers only the
verifier role. Review the transition as an IAM change, create the new grants before relying
on the pipeline, and remove the obsolete verifier grants only after a signing canary passes.

Every entry in `exempt_images` is a hole by design, so the list is an output as well as an
input — it shows up in a plan diff.

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
| <a name="input_attestor_key_ring"></a> [attestor\_key\_ring](#input\_attestor\_key\_ring) | Fully qualified KMS key ring that holds the attestor signing keys. Asymmetric SIGN keys, which is why they are not declared alongside the symmetric keys in the kms module. | `string` | n/a | yes |
| <a name="input_attestor_signers"></a> [attestor\_signers](#input\_attestor\_signers) | Who may create an attestation, keyed by attestor name. Separate from who may deploy.<br/><br/>An empty list is meaningful and is not the same as omitting the entry: it declares that<br/>only a human granted the role out of band may sign, which is how the biosecurity attestor<br/>stays un-automatable. | `map(list(string))` | `{}` | no |
| <a name="input_attestor_verifiers"></a> [attestor\_verifiers](#input\_attestor\_verifiers) | Binary Authorization verifier principals keyed by attestor, normally deployer-project service agents; kept separate from signers | `map(set(string))` | `{}` | no |
| <a name="input_attestors"></a> [attestors](#input\_attestors) | Attestors to create, keyed by short name. Each gets a Container Analysis note and an<br/>asymmetric signing key in attestor\_key\_ring. | <pre>map(object({<br/>    description       = string<br/>    kms_protection    = optional(string, "HSM")<br/>    kms_key_algorithm = optional(string, "RSA_SIGN_PKCS1_4096_SHA512")<br/>  }))</pre> | `{}` | no |
| <a name="input_cluster"></a> [cluster](#input\_cluster) | Cluster the per-namespace rules apply to, as <location>.<cluster\_name>. Google matches<br/>this string literally: a zonal cluster written with its region, or a name that does not<br/>exist, produces a policy that applies to nothing and reports no error. | `string` | `null` | no |
| <a name="input_cluster_admission_rules"></a> [cluster\_admission\_rules](#input\_cluster\_admission\_rules) | Per-namespace exceptions, keyed by namespace. Each is a decision somebody made rather<br/>than a gap somebody left, which is why they are declared rather than defaulted. | <pre>map(object({<br/>    evaluation_mode         = string<br/>    enforcement_mode        = string<br/>    require_attestations_by = optional(list(string), [])<br/>  }))</pre> | `{}` | no |
| <a name="input_default_admission_rule"></a> [default\_admission\_rule](#input\_default\_admission\_rule) | The rule applied to any pod not matched by a namespace rule. This is the whole control:<br/>a permissive default makes every namespace exception decorative. | <pre>object({<br/>    evaluation_mode         = string<br/>    enforcement_mode        = string<br/>    require_attestations_by = optional(list(string), [])<br/>  })</pre> | n/a | yes |
| <a name="input_exempt_images"></a> [exempt\_images](#input\_exempt\_images) | Image path patterns admitted without attestation. A trailing /* matches one path<br/>component and /** matches any depth — the difference is why a registry exemption that<br/>looks right can still deny a nested path. | `list(string)` | `[]` | no |
| <a name="input_global_policy_evaluation_mode"></a> [global\_policy\_evaluation\_mode](#input\_global\_policy\_evaluation\_mode) | Whether Google's own system images bypass this policy. Disabling it means the cluster<br/>cannot start kube-proxy and the node never becomes ready — a failure that presents as a<br/>networking problem and takes a long time to trace back to here. | `string` | `"ENABLE"` | no |
| <a name="input_labels"></a> [labels](#input\_labels) | Labels applied to the KMS signing keys. The policy and attestor resources carry none. | `map(string)` | `{}` | no |
| <a name="input_project_id"></a> [project\_id](#input\_project\_id) | Project the Binary Authorization policy and its attestors live in | `string` | n/a | yes |
| <a name="input_project_number"></a> [project\_number](#input\_project\_number) | Numeric project number used to grant the Binary Authorization service agent access to attestation occurrences | `string` | n/a | yes |

## Outputs

| Name | Description |
| ---- | ----------- |
| <a name="output_attestor_ids"></a> [attestor\_ids](#output\_attestor\_ids) | Attestor resource ids keyed by short name. |
| <a name="output_attestor_key_ids"></a> [attestor\_key\_ids](#output\_attestor\_key\_ids) | KMS signing key ids keyed by attestor. The private half never leaves KMS; a pipeline signs by calling it. |
| <a name="output_attestor_key_versions"></a> [attestor\_key\_versions](#output\_attestor\_key\_versions) | Signing key version ids keyed by attestor, which is what an attestation records as its public key id. |
| <a name="output_attestor_names"></a> [attestor\_names](#output\_attestor\_names) | Attestor names keyed by short name. These are the values a signing pipeline passes to `gcloud container binauthz attestations sign-and-create --attestor`. |
| <a name="output_enforcement_mode"></a> [enforcement\_mode](#output\_enforcement\_mode) | The default rule's enforcement mode, surfaced so a caller can assert that production is not silently in dry-run. |
| <a name="output_exempt_image_patterns"></a> [exempt\_image\_patterns](#output\_exempt\_image\_patterns) | Image patterns admitted without attestation. Every entry here is a hole by design; the list is an output so it appears in a plan diff. |
| <a name="output_policy_id"></a> [policy\_id](#output\_policy\_id) | Binary Authorization policy resource id. |
| <a name="output_signer_grants"></a> [signer\_grants](#output\_signer\_grants) | Least-privilege note-attacher, occurrence-editor, and KMS signer grants created for attestation producers. |
| <a name="output_verifier_grants"></a> [verifier\_grants](#output\_verifier\_grants) | Attestor verifier grants created for Binary Authorization service agents. |
<!-- END_TF_DOCS -->

# Infra / Terraform / Modules / DNS

Cloud DNS managed zones, record sets, and the inbound server policy.

Exercised in this repository by the non-deployable `tests/fixtures/dns_hub` composition.
Public delegation and environment-private DNS callers belong in the separately controlled
live configuration and consume released module versions rather than a branch reference.

## What it does not do

**It creates no project and no network.** Both are inputs. A DNS module that also created its
project would put zone lifecycle and project lifecycle in one state file, and losing a
delegation as a side effect of a project refactor is not a failure worth leaving reachable.
The project comes from `3-networks/dns-project`; the networks come from
`3-networks/shared-vpc-host`.

**It does not manage Certificate Manager authorization records.** The dedicated
`certificate_manager` module owns only the generated CNAME returned by each regional
`PER_PROJECT_RECORD` authorization. Keeping that dynamic record out of the general DNS
record map prevents two Terraform states from claiming the same owner name.

## Interface

| Input | Purpose |
|---|---|
| `project_id` | Project holding every zone. Created elsewhere. |
| `zones` | Map of zone key → `{ dns_name, visibility, dnssec, networks, public_record_allowlist, records }` |
| `attached_networks` | Default network self-links for private zones |
| `inbound_forwarding` | `{ enabled, name, networks }` — the VPN resolution path |
| `enable_logging` | Query logging, public zones only |

Record keys are **relative to the zone**: `"api"` under `mindclade.ai.` becomes
`api.mindclade.ai.`. Use `""` or `"@"` for the apex. An over-qualified key is rejected by a
variable validation rather than silently producing `api.mindclade.ai.mindclade.ai.`, which
resolves nowhere and reads like a propagation delay.

**One owner, several types.** The map key is an *identifier* that defaults to the owner name;
set `name` explicitly when one owner carries more than one record type. An apex holding CAA,
MX, and SPF needs three entries, and a map cannot hold three `"@"` keys:

```hcl
records = {
  caa = { name = "@", type = "CAA", rrdatas = ["0 issue \"letsencrypt.org\""] }
  mx  = { name = "@", type = "MX",  rrdatas = ["1 smtp.google.com."] }
  spf = { name = "@", type = "TXT", rrdatas = ["v=spf1 include:_spf.google.com -all"] }
}
```

Cloud DNS keys record sets by name **and** type, so these are three distinct sets rather than
a collision. Without the override they would compete for one key and the last one written
would win silently.

## Three guards worth knowing about before you trip one

**Public A, AAAA, and CNAME records require an exact exception.** TXT, CAA, MX, child NS, and
provider-owned SOA remain accepted by default. A reviewed public address exception must put the
exact `records` map key in `public_record_allowlist`; allowing one key never permits another.
The module rejects wildcard owners, stale keys, non-address keys, and allowlists on private
zones. Certificate Manager authorization CNAMEs remain owned by the certificate module rather
than this static exception mechanism.

**A private zone attached to no network is rejected.** Cloud DNS accepts it happily and then
resolves nothing, with no error anywhere. The precondition turns a silent misconfiguration
into a plan-time failure.

**Zones carry `prevent_destroy`; records deliberately do not.** A zone is a delegation you can
lose by refactoring. Records change every time a hostname is added, and a lifecycle guard
there would make each addition a state-surgery exercise.

## The step Terraform cannot finish

`inbound_forwarding` allocates a forwarding target address per attached network, but the
provider does not expose the addresses. Read them back with

```sh
gcloud compute addresses list --filter='purpose=DNS_RESOLVER' \
  --format='table(address, subnetwork, region)'
```

and point the VPN or on-prem resolver at them with a conditional forwarder per private domain.
Until that happens, every name resolves inside the VPC and NXDOMAINs on a laptop — which
presents as a workload bug and is not one.

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
| <a name="input_attached_networks"></a> [attached\_networks](#input\_attached\_networks) | Default network self-links for private zones, used by any zone that does not set its own<br/>`networks`.<br/><br/>Attachment is what makes a private zone visible. A private zone attached to no network is<br/>not an error — it resolves nowhere, silently. A zone attached to a subset of environments<br/>is worse: the name works in one VPC and NXDOMAINs in another, which reads as a workload<br/>bug rather than a DNS one. | `list(string)` | `[]` | no |
| <a name="input_enable_logging"></a> [enable\_logging](#input\_enable\_logging) | Query logging on public zones.<br/><br/>Low volume next to flow logs, and the one record that answers "what did this try to<br/>resolve" when a name fails. Public zones only — private-zone query logging is a property<br/>of the network's DNS policy, not the zone. | `bool` | `false` | no |
| <a name="input_inbound_forwarding"></a> [inbound\_forwarding](#input\_inbound\_forwarding) | Inbound server policy: a forwarding target address inside the VPC that an on-prem or VPN<br/>resolver can be pointed at with a conditional forwarder.<br/><br/>This is the piece most often missed. Without it every private zone resolves correctly from<br/>inside the VPC and NXDOMAINs from a laptop on the VPN — and the symptom, "works in the<br/>cluster, not on my machine", sends people looking at the workload. | <pre>object({<br/>    enabled  = optional(bool, false)<br/>    name     = optional(string, "inbound-forwarding")<br/>    networks = optional(list(string))<br/>  })</pre> | `{}` | no |
| <a name="input_labels"></a> [labels](#input\_labels) | Labels applied to every zone, merged over the module's own baseline. | `map(string)` | `{}` | no |
| <a name="input_owner"></a> [owner](#input\_owner) | Team accountable for these zones. Applied as a label so an unexpected zone has someone to ask. | `string` | `"platform"` | no |
| <a name="input_project_id"></a> [project\_id](#input\_project\_id) | Project holding every zone and policy this module creates. Created elsewhere; this module never creates a project. | `string` | n/a | yes |
| <a name="input_zones"></a> [zones](#input\_zones) | Managed zones, keyed by a short name that becomes the Cloud DNS zone name.<br/><br/>`records` is keyed by the owner name RELATIVE to the zone — "api" under<br/>"mindclade.ai." becomes "api.mindclade.ai.". Use "" or "@" for the apex. Passing an<br/>already-qualified name is rejected rather than silently producing<br/>"api.mindclade.ai.mindclade.ai.", which resolves nowhere and looks like a propagation<br/>delay.<br/><br/>`dnssec` defaults per visibility: on for public, off for private. DNSSEC on a private zone<br/>signs against a resolver that was never untrusted, and costs a key that has to be rotated. | <pre>map(object({<br/>    dns_name = string<br/><br/>    # Falls back to a generated string. Not defaulted to "" — Cloud DNS rejects an empty<br/>    # description, and the error names the field without naming the zone.<br/>    description = optional(string)<br/><br/>    visibility = optional(string, "private")<br/>    dnssec     = optional(bool)<br/><br/>    # Overrides var.attached_networks for this zone only. Ignored on public zones.<br/>    networks = optional(list(string))<br/><br/>    # Exact records-map keys for reviewed public A, AAAA, or CNAME exceptions. Public<br/>    # address records remain denied by default; private zones may not use this escape hatch.<br/>    public_record_allowlist = optional(set(string), [])<br/><br/>    # Keyed by an identifier that DEFAULTS to the relative owner name. Set<br/>    # `name` explicitly when one owner carries more than one type -- an apex<br/>    # holding CAA, MX, and SPF needs three entries that resolve to the same<br/>    # name, and a map cannot hold three "@" keys. Cloud DNS keys record sets by<br/>    # name AND type, so these are three distinct sets rather than a collision.<br/>    records = optional(map(object({<br/>      name    = optional(string)<br/>      type    = string<br/>      ttl     = optional(number, 300)<br/>      rrdatas = list(string)<br/>    })), {})<br/>  }))</pre> | n/a | yes |

## Outputs

| Name | Description |
| ---- | ----------- |
| <a name="output_inbound_policy_name"></a> [inbound\_policy\_name](#output\_inbound\_policy\_name) | Name of the inbound server policy, or null when inbound forwarding is disabled.<br/><br/>The forwarding target ADDRESSES are deliberately not an output: Cloud DNS allocates one<br/>per attached network, and the Terraform provider does not expose them on this resource.<br/>Read them back with<br/><br/>  gcloud compute addresses list --filter='purpose=DNS\_RESOLVER' \<br/>    --format='table(address, subnetwork, region)'<br/><br/>then configure the on-prem or VPN resolver with a conditional forwarder pointing at them<br/>for each private domain. That step is outside Terraform, which is exactly why it is the<br/>one most often skipped — and skipping it produces names that resolve in-cluster and<br/>NXDOMAIN on a laptop, with nothing in this state file to suggest why. |
| <a name="output_name_servers"></a> [name\_servers](#output\_name\_servers) | Delegation name servers by map key, for public zones.<br/><br/>These are what the registrar has to be pointed at. A public zone whose registrar still<br/>delegates elsewhere validates nothing and issues no certificate, and the ACME failure<br/>names the challenge rather than the delegation. |
| <a name="output_zone_ids"></a> [zone\_ids](#output\_zone\_ids) | Fully qualified zone ids by map key. |
| <a name="output_zone_names"></a> [zone\_names](#output\_zone\_names) | Cloud DNS zone name by map key. |
<!-- END_TF_DOCS -->

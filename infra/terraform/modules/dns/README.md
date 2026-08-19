# Infra / Terraform / Modules / DNS

Cloud DNS managed zones, record sets, and the inbound server policy.

Consumed by `infrastructure-live/3-networks/dns-hub`, which is the only caller. Pinned by
semver tag from there, never by branch.

## What it does not do

**It creates no project and no network.** Both are inputs. A DNS module that also created its
project would put zone lifecycle and project lifecycle in one state file, and losing a
delegation as a side effect of a project refactor is not a failure worth leaving reachable.
The project comes from `3-networks/dns-project`; the networks come from
`3-networks/shared-vpc-host`.

**It does not manage `_acme-challenge` TXT records.** cert-manager writes and removes those
during each DNS-01 challenge. A Terraform-owned record at that name either fights the solver
or blocks issuance outright.

Related: do not create a Certificate Manager DNS authorization for any zone here. That
mechanism uses a **CNAME** and requires it to be the only record at the name, so the two
cannot share `_acme-challenge`.

## Interface

| Input | Purpose |
|---|---|
| `project_id` | Project holding every zone. Created elsewhere. |
| `zones` | Map of zone key → `{ dns_name, visibility, dnssec, networks, records }` |
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

**A public zone may carry only TXT, CAA, MX, or NS.** Every application hostname in this
estate resolves privately and has no public address record; an `A` on a public zone would undo
that in one apply, and nothing else would report it. The validation is the control — the DNS
posture in the design document is otherwise one `terraform apply` from being convention.

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

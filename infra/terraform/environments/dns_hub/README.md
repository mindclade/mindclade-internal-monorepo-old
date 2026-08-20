# DNS hub

The estate's four public Cloud DNS zones, and nothing else.

This is the **first thing to apply** and the only Terraform root that can be
applied before a VPC, a cluster, or a GKE node exists. Everything else in the
platform's TLS story is blocked on it: cert-manager solves ACME DNS-01 by
writing a TXT record into these zones, so no certificate can be issued until the
registrar delegates here.

```
infra/terraform/environments/dns_hub/
├── main.tf                    zone + record construction
├── variables.tf               every input; nothing estate-specific is hardcoded
├── terraform.tfvars.example   the filled-in form for mindclade.*
├── versions.tf                provider pins, matching modules/dns
├── backend.tf                 partial GCS backend (bucket supplied at init)
├── bootstrap.sh               project, state bucket, and Terraform SA
├── tests/                     terraform test suite (mock provider, no credentials)
└── outputs.tf                 name servers + ready-to-paste dig commands
```

## Why this is a separate root

A registrable domain is not per-environment. `mindclade.ai` is one delegation
shared by development, staging, and production alike — the registrar can be
pointed at exactly one set of name servers. Splitting the zone across three
state files would produce three answers to a question that has one.

The split here is by **lifetime**, not by environment: these zones outlive every
cluster that resolves through them.

## What it publishes, and what it deliberately does not

Per domain, a successful apply publishes exactly two things: **the delegation**
and **the CAA policy** naming who may issue for it.

There are **no A, AAAA, or CNAME records**, and `modules/dns` rejects them on a
public zone by validation. Every application hostname in this estate resolves
through *private* zones; one public address record would undo that in a single
apply, with no other signal. Private zones need a VPC and land in a later slice.

There are **no `_acme-challenge` records**. cert-manager creates and removes
those during each challenge; a Terraform-owned record at that name fights the
solver, and the conflict surfaces as an intermittent validation failure.

## CAA, and why `wildcard_certificates` matters

`issue` names the CAs allowed to issue a normal certificate. `issuewild` governs
wildcards **separately**, and a CA consults it *instead of* `issue` for a
wildcard request rather than in addition to it — so it has to be stated even
when the answer is the same list.

Setting `wildcard_certificates = false` publishes `0 issuewild ";"`, the CAA
idiom for *no CA may issue a wildcard here*. On `mindclade.studio` that is not
decoration: `cert-manager/base/certificates.yaml` requires that crt.sh show
`mindclade.studio` and **never** `*.mindclade.studio`. Without this record that
requirement is a convention one cert-manager edit can undo; with it, Let's
Encrypt enforces it on our behalf.

This file and `certificates.yaml` must agree in both directions, and
`tools/analysis/check_dns_caa_alignment.py` enforces it in presubmit rather than
leaving it to review. The two failure modes are not symmetric, which is why it
is a check:

- **Requested but forbidden** — a `Certificate` asks for a wildcard CAA denies.
  Let's Encrypt refuses. Loud, but *late*: nothing fails until issuance is
  attempted, and for a renewal that can be sixty days after the commit.
- **Permitted but unrequested** — CAA allows a wildcard nothing asks for. This
  never fails and never expires. It is a standing authorisation for anyone who
  can pass a domain-control challenge, sitting in a public record.

## Mail posture

The three domains that receive no mail get a null MX (`0 .`, RFC 7505), a
reject-all SPF, and `p=reject` DMARC. This is load-bearing: **absent records are
read by receivers as "unconfigured", not as "sends no mail"**, which is what
makes an unused domain attractive for spoofing.

`mindclade.com` is the mail domain because the ACME account address
(`security@mindclade.com`) lives there. Until `mail.mx` is set it gets the same
reject-all posture as the others — safe, but it means that address **bounces**.
Fill in `mail` before requesting the first production certificate, or Let's
Encrypt's expiry warnings go nowhere.

## Test

```bash
cd infra/terraform/environments/dns_hub
terraform init -backend=false
terraform test -var-file=terraform.tfvars.example
```

The var-file is not optional. One run block deliberately declares no `variables`
so that it evaluates the **committed example** — real `terraform.tfvars` is
gitignored, so without this nothing ever executes these values and the example
rots silently. Presubmit runs exactly this command.

`terraform console -var-file=...` is not a substitute: it evaluates variables
lazily, so a trivial expression exits 0 against inputs that violate every
validation in `variables.tf`. `terraform test` plans through a mock provider, so
every variable is evaluated, every validation fires, and no credentials are
needed.

`terraform output zone_records` (or the same value in a plan) is the readable
form of what will be published — CAA and SPF are exactly the records where a
wrong character is invisible in a resource diff and expensive in production.

## Apply

The bucket is not committed — Terraform reads the backend block before it
evaluates variables, so it cannot be an input. Supply it at init:

```bash
cd infra/terraform/environments/dns_hub
cp terraform.tfvars.example terraform.tfvars   # gitignored; edit if needed

terraform init -backend-config=bucket=<state-bucket>
terraform plan
terraform apply
```

Set `impersonate_service_account` as soon as a Terraform service account
exists. Left null, every apply runs as whoever is at the keyboard, which makes
the delegation for every domain the company owns reachable from any laptop that
has run `gcloud auth application-default login`. It starts null only because
this root is the first thing applied in a new estate, when a human's credentials
are the only ones there are.

Use a bucket with **object versioning enabled**. This state holds the delegation
for every domain the company owns; recovering from a bad apply is a restore.

The project must already exist — neither this root nor `modules/dns` creates
one, so that losing a delegation cannot be a side effect of a project refactor.
Nor can Terraform create the bucket its own state lives in, or the service
account it runs as. `bootstrap.sh` does all three, idempotently:

```bash
BILLING_ACCOUNT=XXXXXX-XXXXXX-XXXXXX ./bootstrap.sh
```

It is safe to re-run: every step checks for the resource first, nothing is
deleted, and a partial failure can be fixed and the script run again. Set
`ORGANIZATION_ID` or `FOLDER_ID` to place the project under a parent, and
`PROJECT_ID` / `STATE_BUCKET` to use different names. It stops rather than
continuing if no billing account is linked, because Cloud DNS cannot be enabled
without one and a project that looks bootstrapped but is not is worse than a
failure.

It creates no service account **key**. The grant is
`roles/iam.serviceAccountTokenCreator` to your own account, so applies mint a
short-lived token rather than relying on a private key that lives until someone
remembers to rotate it.

`project_id` must match `solvers[].dns01.cloudDNS.project` in
`infra/kubernetes/platform/cert-manager/base/issuer.yaml`. cert-manager looks
the zone up by domain name **inside that project**; a mismatch fails as a
challenge TXT record that is never written.

## Delegating from Squarespace

Squarespace sells the domain but will not be answering for it. Registrar
nameservers have to point at Cloud DNS, and Squarespace's own DNS records stop
having any effect the moment they do.

1. Get the name servers this apply produced:

   ```bash
   terraform output -json name_servers
   ```

   Four per zone, each ending in a dot, e.g. `ns-cloud-a1.googledomains.com.`

2. In Squarespace: **Domains → \<domain\> → DNS → Nameservers**, choose *Use
   custom nameservers*, and enter all four. Drop the trailing dot — Squarespace
   supplies it. Repeat per domain; the four sets are **different**, and reusing
   one zone's name servers for another domain is the most common failure here.
   It presents as NXDOMAIN for every name on the wrong domain.

3. Squarespace will warn that existing DNS records will stop working. For these
   domains that is the intent.

Delegation propagates in minutes to ~48 hours, bounded by the TTL of the old
`NS` records at the TLD.

## Verify

Ask the **public internet**, not Cloud DNS. `gcloud dns` only proves Terraform
applied; these prove the delegation is live, and the two can be days apart:

```bash
terraform output -json delegation_check   # per-domain dig NS commands
terraform output -json caa_check          # per-domain dig CAA commands
```

Delegation — should return the Google name servers, not Squarespace's:

```bash
dig +short NS mindclade.studio @1.1.1.1
```

CAA — should list only Let's Encrypt, and on `.studio`/`.com` also show
`issuewild ";"`:

```bash
dig +short CAA mindclade.studio @1.1.1.1
```

Mail posture on a non-mail domain — should be exactly `0 .`:

```bash
dig +short MX mindclade.ai @1.1.1.1
```

## DNSSEC

DNSSEC is enabled on all four zones, but a signed zone does nothing until the
**DS record is published at the registrar** — that is the link that ties the
zone's key to the parent TLD. Until then the zone is signed and no resolver
validates it.

Squarespace supports this. Switching a domain to custom nameservers
automatically disables Squarespace's own DNSSEC (their signing is for their
nameservers, which are no longer answering), after which you can add up to eight
DS or DNSKEY records from a third-party provider.

**Order matters, and getting it wrong takes the domain down.** A DS record that
does not match the zone's actual key makes every validating resolver return
SERVFAIL for the entire domain — not a degraded answer, no answer. So:

1. Delegate first and confirm `dig NS` answers with the Google name servers.
2. Only then read the DS data out of Cloud DNS:

   ```bash
   gcloud dns dns-keys list --zone=studio --project=mc-common-dns \
     --filter="type=keySigning" --format="value(ds_record())"
   ```

3. Paste it into **Domains → \<domain\> → DNS → DNSSEC** in Squarespace.
4. Verify the chain actually validates — `dig` alone will not tell you:

   ```bash
   dig +short DS mindclade.studio @1.1.1.1        # the record exists
   dig +dnssec +cd mindclade.studio @1.1.1.1      # signatures are present
   ```

   For a real chain-of-trust check use <https://dnsviz.net/> or
   <https://dnssec-analyzer.verisignlabs.com/>, which walk the delegation from
   the root and report a broken link explicitly.

**Before ever deleting a zone or migrating a domain away, remove the DS record
first** and wait for its TTL to expire. Deleting a signed zone while the parent
still advertises its DS is the same outage as publishing a wrong one, and the
zone carries `prevent_destroy` precisely because that mistake is unrecoverable
in the time anyone would like.

## Next

Once `dig NS` answers with Google name servers for all four domains, the
delegation is live and ACME DNS-01 can succeed. The remaining prerequisite for
an actual certificate is the workload identity binding granting the
cert-manager service account `roles/dns.admin` **on these zones only** — not
project-wide, since the credential's whole job is writing `_acme-challenge`
records and a project-wide grant would also let a compromised solver rewrite the
private zones every hostname resolves through.

<!-- BEGIN_TF_DOCS -->
## Requirements

| Name | Version |
| ---- | ------- |
| <a name="requirement_terraform"></a> [terraform](#requirement\_terraform) | >= 1.9.0, < 2.0.0 |
| <a name="requirement_google"></a> [google](#requirement\_google) | >= 7.41.0, < 8.0.0 |

## Providers

No providers.

## Inputs

| Name | Description | Type | Default | Required |
| ---- | ----------- | ---- | ------- | :------: |
| <a name="input_certificate_authorities"></a> [certificate\_authorities](#input\_certificate\_authorities) | CAs permitted to issue for these domains, as CAA `issue` values.<br/><br/>CAA is the only control here that binds a third party: with it, a CA that is<br/>not listed must refuse an issuance request even from someone who has proven<br/>domain control. Without it, any of the ~90 public CAs will issue to whoever<br/>passes their challenge.<br/><br/>cert-manager's ACME solver is Let's Encrypt, so the default is exactly that<br/>and nothing else. | `list(string)` | <pre>[<br/>  "letsencrypt.org"<br/>]</pre> | no |
| <a name="input_domains"></a> [domains](#input\_domains) | The estate's registrable domains, keyed by the role each one plays. The key<br/>becomes the Cloud DNS zone name, so it is a short identifier rather than a<br/>hostname.<br/><br/>`wildcard_certificates` controls the CAA posture and nothing else:<br/><br/>  true  -- the plane serves names below the apex, so a CA may issue a<br/>           wildcard for it.<br/>  false -- CAA actively FORBIDS wildcard issuance (`issuewild ";"`). This is<br/>           not the same as declining to request one: it stops any CA from<br/>           issuing a wildcard for the domain even if someone later asks,<br/>           and the refusal is visible in Certificate Transparency.<br/><br/>`mail` is set only on the domain that receives mail. A domain with no mail<br/>gets a null-MX and a reject-all SPF, which is what makes it unusable for<br/>spoofing rather than merely unused. | <pre>map(object({<br/>    dns_name              = string<br/>    description           = optional(string)<br/>    wildcard_certificates = optional(bool, true)<br/>    dnssec                = optional(bool, true)<br/>  }))</pre> | n/a | yes |
| <a name="input_enable_logging"></a> [enable\_logging](#input\_enable\_logging) | Cloud DNS query logging on public zones.<br/><br/>Low volume next to VPC flow logs, and the only record that answers "what did<br/>this actually try to resolve" when a name fails. Off by default because it<br/>is billable. | `bool` | `false` | no |
| <a name="input_impersonate_service_account"></a> [impersonate\_service\_account](#input\_impersonate\_service\_account) | Service account to impersonate for every API call, or null to use the<br/>caller's own credentials.<br/><br/>At this stage the DNS host project is the most privileged thing in the<br/>estate: it holds the delegation for every domain the company owns, and a<br/>change here is the one change that can take every hostname dark. Applying<br/>with a human's application-default credentials makes that reachable from any<br/>laptop that has run `gcloud auth application-default login`, with the audit<br/>log naming a person rather than a pipeline.<br/><br/>Impersonation moves the authority onto a service account that a human holds<br/>roles/iam.serviceAccountTokenCreator on. That grant is revocable in one<br/>place, it expires with the token rather than with the laptop, and every<br/>mutation is attributed to the pipeline identity.<br/><br/>Left null by default because it cannot be set before the service account<br/>exists, and this root is the FIRST thing applied in a new estate -- there is<br/>a bootstrap apply where the only available credential is a human's. Set it<br/>immediately after. | `string` | `null` | no |
| <a name="input_labels"></a> [labels](#input\_labels) | Additional labels applied to every zone. | `map(string)` | `{}` | no |
| <a name="input_mail"></a> [mail](#input\_mail) | Mail records for `mail_domain`. Left empty until a provider is chosen; the<br/>zone still applies, so delegation is never blocked on picking one.<br/><br/>`mx` entries are full Cloud DNS rrdata strings, "<preference> <host.>", with<br/>the trailing dot. Google Workspace is one entry: "1 smtp.google.com.".<br/><br/>`spf_include` is the provider's include mechanism -- "\_spf.google.com" for<br/>Workspace. The record is assembled here rather than pasted so it cannot<br/>accumulate two `all` mechanisms, which silently voids the whole policy. | <pre>object({<br/>    mx          = optional(list(string), [])<br/>    spf_include = optional(list(string), [])<br/><br/>    # Starts at none so a misconfigured selector cannot bounce real mail on day<br/>    # one. Move to quarantine, then reject, once the DMARC reports are clean.<br/>    dmarc_policy = optional(string, "none")<br/>    dmarc_rua    = optional(string)<br/><br/>    # DKIM is emitted verbatim: the value is a provider-generated public key,<br/>    # and there is nothing useful to validate about it here.<br/>    dkim = optional(map(object({<br/>      selector = string<br/>      value    = string<br/>    })), {})<br/>  })</pre> | `{}` | no |
| <a name="input_mail_domain"></a> [mail\_domain](#input\_mail\_domain) | Key in `domains` that receives mail, or null when the estate has none.<br/><br/>This is the domain the ACME account address belongs to. Let's Encrypt sends<br/>expiry warnings there and the address is awkward to change afterwards, so<br/>its MX has to be live BEFORE the first certificate is requested -- which is<br/>why mail records and zone delegation land in the same apply. | `string` | `null` | no |
| <a name="input_owner"></a> [owner](#input\_owner) | Team accountable for these zones. Applied as a label so an unexpected zone has someone to ask. | `string` | `"platform"` | no |
| <a name="input_project_id"></a> [project\_id](#input\_project\_id) | Project that will hold every public zone.<br/><br/>Created elsewhere -- neither this root nor the DNS module creates a project,<br/>so that losing a delegation cannot be a side effect of a project refactor.<br/>Bootstrap it once with `gcloud projects create`, then pass the id here. | `string` | n/a | yes |
| <a name="input_record_ttl"></a> [record\_ttl](#input\_record\_ttl) | TTL for every record this root publishes.<br/><br/>An hour is right for records that change on the order of never: CAA, MX,<br/>SPF, DMARC. It is deliberately NOT the TTL for anything that fails over --<br/>no such record lives in this root, because every address record in the<br/>estate is private. Lower it temporarily before a planned mail migration, so<br/>the cutover is not gated on caches holding the old MX. | `number` | `3600` | no |
| <a name="input_security_contact"></a> [security\_contact](#input\_security\_contact) | Address published in each zone's CAA `iodef` record, where a CA reports a<br/>policy violation it refused.<br/><br/>Distinct from the ACME account address in intent even when the mailbox is<br/>the same: this one is read by a CA telling you someone tried to get a<br/>certificate they should not have. | `string` | n/a | yes |

## Outputs

| Name | Description |
| ---- | ----------- |
| <a name="output_caa_check"></a> [caa\_check](#output\_caa\_check) | Per-domain CAA verification. Should list only the CAs in<br/>var.certificate\_authorities; on apex-only domains it should also show<br/>`issuewild ";"`, which is the record that forbids wildcard issuance. |
| <a name="output_delegation_check"></a> [delegation\_check](#output\_delegation\_check) | Ready-to-paste command per domain that asks the PUBLIC internet, not Cloud<br/>DNS, who is authoritative.<br/><br/>Checking with `gcloud dns` instead only proves Terraform applied. This is<br/>the one that distinguishes "the zone exists" from "the zone is live", and<br/>they are days apart when a registrar change is still propagating. |
| <a name="output_name_servers"></a> [name\_servers](#output\_name\_servers) | Delegation name servers per domain. THIS IS THE POINT OF THE APPLY.<br/><br/>Every one of these has to be entered at the registrar before anything else<br/>in the estate works. Until the registrar delegates here, this zone is a<br/>correct set of records that no resolver on the internet consults, and the<br/>first symptom is an ACME challenge that times out rather than one that is<br/>refused -- which reads as a cert-manager problem.<br/><br/>Print them with:  terraform output -json name\_servers |
| <a name="output_project_id"></a> [project\_id](#output\_project\_id) | Project holding the zones. cert-manager's DNS-01 solver needs it, and its<br/>workload identity binding grants roles/dns.admin here rather than in the<br/>cluster's own project. |
| <a name="output_zone_names"></a> [zone\_names](#output\_zone\_names) | Cloud DNS zone name per domain, for `gcloud dns record-sets list --zone=...`<br/>and for the cert-manager ClusterIssuer's DNS-01 solver configuration. |
| <a name="output_zone_records"></a> [zone\_records](#output\_zone\_records) | Every record this root will publish, keyed by zone and then by record<br/>identifier, with owner names still RELATIVE to the zone.<br/><br/>Two uses. Reviewing a plan by reading `google_dns_record_set` diffs means<br/>reading forty lines of provider noise to answer "what will DNS actually<br/>say"; this answers it directly, and CAA and SPF are exactly the records<br/>where a wrong character is invisible in a diff and expensive in production.<br/><br/>It is also what makes this root testable. `terraform test` can assert on a<br/>root output but cannot reach resources inside a child module, so without<br/>this the CAA and mail logic here could only be verified by applying it. |
<!-- END_TF_DOCS -->

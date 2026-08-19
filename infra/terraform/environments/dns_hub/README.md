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
one, so that losing a delegation cannot be a side effect of a project refactor:

```bash
gcloud projects create mc-common-dns --name="Mindclade DNS"
gcloud services enable dns.googleapis.com --project=mc-common-dns
```

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

DNSSEC is enabled on all four zones. The DS record has to be published **at the
registrar** to have any effect, and Squarespace may not support it; an unsigned
delegation is not an error, just an unrealised control. Check with:

```bash
dig +short DS mindclade.studio @1.1.1.1
```

## Next

Once `dig NS` answers with Google name servers for all four domains, the
delegation is live and ACME DNS-01 can succeed. The remaining prerequisite for
an actual certificate is the workload identity binding granting the
cert-manager service account `roles/dns.admin` **on these zones only** — not
project-wide, since the credential's whole job is writing `_acme-challenge`
records and a project-wide grant would also let a compromised solver rewrite the
private zones every hostname resolves through.

# `essential_contacts`

Subscribes folder- and project-scoped Essential Contacts.

`bootstrap` owns the organization-level contacts — the addresses that must receive a
suspension notice or a security bulletin regardless of who owns what. This module is for the
routing *below* that, and the reason it exists is legibility: an org-level contact receives
everything for everywhere, so a GPU quota warning in `development` and a suspension notice on
`production` land in the same inbox with the same weight. Within a month the filter rule
somebody wrote to cope means neither is read.

Do not declare the same contact here and in `bootstrap`. Two configurations declaring one
contact each see the other's value as drift and revert it on every apply.

## Interface notes

- **Keys are `<parent>:<email>`, not list indices.** An index key moves every contact below an
  insertion, destroying and recreating contacts that did not change.
- **`ALL` beside another category is rejected.** It is not additive — it is the same delivery
  with a wider net, and listing it next to `TECHNICAL` makes the narrower entry read as if it
  constrains something.
- **A contact with no subscriptions is rejected.** It receives nothing; omit it instead.

Addresses should be groups. A contact pointing at a person stops working the day they leave,
and nobody notices until a notification is missed — which is exactly the notification that
mattered. That cannot be checked from an address alone, so it is enforced upstream by the
`essentialcontacts.allowedContactDomains` org policy rather than pretended at here.

## Usage

```hcl
module "contacts" {
  source = "git::https://github.com/mindclade-org/mindclade.git//infra/terraform/modules/essential_contacts?ref=v0.2.0"

  contacts = {
    "folders/000000000000" = [
      { email = "platform@mindclade.com", subscriptions = ["TECHNICAL", "SUSPENSION"] },
      { email = "legal@mindclade.com", subscriptions = ["LEGAL"] },
    ]
  }
}
```

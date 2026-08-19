#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""CAA policy in Terraform must agree with what cert-manager actually requests.

Two files decide whether a certificate can be issued, and nothing connects them:

  infra/terraform/environments/dns_hub/terraform.tfvars.example
      `wildcard_certificates` per domain, which becomes the CAA `issuewild`
      record -- the permission, enforced by the CA.

  infra/kubernetes/platform/cert-manager/base/certificates.yaml
      `dnsNames` per Certificate -- the request.

Drift between them is asymmetric, which is why it needs a check rather than a
convention:

  Requested but forbidden -- a Certificate asks for `*.example.com` while CAA
  says `issuewild ";"`. Let's Encrypt refuses. This is loud but LATE: nothing
  fails until an issuance is attempted, and for a renewal that can be sixty days
  after the commit that caused it, by which point the change is not a suspect.

  Permitted but unrequested -- CAA allows a wildcard no Certificate asks for.
  This NEVER fails. It is a standing authorisation for any actor who can pass a
  domain-control challenge to obtain a wildcard, sitting in a public record with
  nothing to draw attention to it. mindclade.studio is the case the estate cares
  about: certificates.yaml requires that crt.sh show `mindclade.studio` and
  never `*.mindclade.studio`, and only CAA makes that a rule a third party
  enforces.

Parsing is deliberately hand-rolled. Every other check in this directory is
stdlib-only, and adding PyYAML to the presubmit's dependency surface to read one
list of strings is a bad trade.
"""

from __future__ import annotations

import re
from pathlib import Path

TFVARS = "infra/terraform/environments/dns_hub/terraform.tfvars.example"
CERTIFICATES = "infra/kubernetes/platform/cert-manager/base/certificates.yaml"

# Mirrors `optional(bool, true)` in the dns_hub variables. If that default ever
# changes, this constant is the thing that has to change with it -- a domain
# block that omits the field would otherwise be judged against the wrong policy.
WILDCARD_DEFAULT = True


def _balanced(text: str, open_brace: int) -> int:
    """Index of the `}` closing the `{` at open_brace, or len(text)."""
    depth = 0
    for index in range(open_brace, len(text)):
        if text[index] == "{":
            depth += 1
        elif text[index] == "}":
            depth -= 1
            if depth == 0:
                return index
    return len(text)


def _parse_domains(text: str) -> dict[str, dict]:
    """Map domain key -> {"dns_name": "example.com", "wildcard": bool}."""
    start = re.search(r"^domains\s*=\s*\{", text, re.MULTILINE)
    if not start:
        return {}
    body = text[start.end() : _balanced(text, start.end() - 1)]

    domains: dict[str, dict] = {}
    for match in re.finditer(r"(\w+)\s*=\s*\{", body):
        block = body[match.end() : _balanced(body, match.end() - 1)]
        name = re.search(r'dns_name\s*=\s*"([^"]+)"', block)
        if not name:
            continue
        wildcard = re.search(r"wildcard_certificates\s*=\s*(true|false)", block)
        domains[match.group(1)] = {
            # Zone names are fully qualified; SANs are not.
            "dns_name": name.group(1).rstrip("."),
            "wildcard": WILDCARD_DEFAULT if not wildcard else wildcard.group(1) == "true",
        }
    return domains


def _parse_certificates(text: str) -> list[tuple[str, list[str]]]:
    """Return (certificate name, dnsNames) for each Certificate document."""
    certificates = []
    for document in re.split(r"^---\s*$", text, flags=re.MULTILINE):
        if not re.search(r"^kind:\s*Certificate\s*$", document, re.MULTILINE):
            continue
        name = re.search(r"^\s{2}name:\s*(\S+)\s*$", document, re.MULTILINE)
        names: list[str] = []
        block = re.search(r"^(\s*)dnsNames:\s*$", document, re.MULTILINE)
        if block:
            indent = len(block.group(1))
            for line in document[block.start() :].splitlines()[1:]:
                if not line.strip():
                    continue
                stripped = line.lstrip()
                # A list item indented past the key continues the block; the
                # first line at or left of the key's indent ends it.
                if not stripped.startswith("- ") or (len(line) - len(stripped)) <= indent:
                    break
                names.append(stripped[2:].strip().strip("\"'"))
        certificates.append((name.group(1) if name else "<unnamed>", names))
    return certificates


def check(root: Path) -> list[str]:
    tfvars, certificates = root / TFVARS, root / CERTIFICATES
    for path, rel in ((tfvars, TFVARS), (certificates, CERTIFICATES)):
        if not path.exists():
            return [f"missing {rel}"]

    domains = _parse_domains(tfvars.read_text(errors="replace"))
    if not domains:
        return [f"{TFVARS}: parsed no domains; the CAA alignment check is not actually running"]

    parsed = _parse_certificates(certificates.read_text(errors="replace"))
    if not parsed:
        return [
            f"{CERTIFICATES}: parsed no Certificates; the CAA alignment check is not actually running"
        ]

    by_apex = {d["dns_name"]: (key, d["wildcard"]) for key, d in domains.items()}
    errors: list[str] = []
    wildcards_requested: set[str] = set()

    for certificate, names in parsed:
        if not names:
            errors.append(f"{CERTIFICATES}: Certificate {certificate} declares no dnsNames")
            continue
        for san in names:
            apex = san[2:] if san.startswith("*.") else san
            if apex not in by_apex:
                errors.append(
                    f"{CERTIFICATES}: Certificate {certificate} requests {san!r}, but no zone "
                    f"in {TFVARS} serves {apex!r}. DNS-01 cannot solve a challenge in a zone "
                    f"that does not exist, so this certificate can never be issued."
                )
                continue
            key, wildcard_allowed = by_apex[apex]
            if san.startswith("*."):
                wildcards_requested.add(apex)
                if not wildcard_allowed:
                    errors.append(
                        f"Certificate {certificate} requests {san!r}, but {TFVARS} sets "
                        f"domains.{key}.wildcard_certificates = false, which publishes CAA "
                        f"'0 issuewild \";\"'. Let's Encrypt will REFUSE this issuance. Set "
                        f"wildcard_certificates = true, or drop the wildcard SAN."
                    )

    for apex, (key, wildcard_allowed) in sorted(by_apex.items()):
        if wildcard_allowed and apex not in wildcards_requested:
            errors.append(
                f"{TFVARS}: domains.{key}.wildcard_certificates = true permits a wildcard for "
                f"{apex!r}, but no Certificate in {CERTIFICATES} requests one. This never fails "
                f"and never expires: it is a standing authorisation for anyone who can pass a "
                f"domain-control challenge. Set it to false to publish 'issuewild \";\"'."
            )

    return errors


if __name__ == "__main__":
    found = check(Path(__file__).resolve().parents[2])
    for error in found:
        print(error)
    print("PASS CAA/cert-manager alignment" if not found else f"{len(found)} error(s)")
    raise SystemExit(1 if found else 0)

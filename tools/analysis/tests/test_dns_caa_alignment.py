# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""The CAA alignment check has to FAIL on drift, or it is decoration.

Each case builds a two-file fixture and mutates one side, so a passing test
proves the check reads both files rather than one.
"""

from __future__ import annotations

import importlib.util
import sys
import tempfile
from pathlib import Path

ROOT = Path(__file__).resolve().parents[3]


def load(name: str, path: Path):
    spec = importlib.util.spec_from_file_location(name, path)
    if spec is None or spec.loader is None:
        raise RuntimeError(f"unable to load {path}")
    module = importlib.util.module_from_spec(spec)
    sys.modules[name] = module
    spec.loader.exec_module(module)
    return module


alignment = load("check_dns_caa_alignment", ROOT / "tools/analysis/check_dns_caa_alignment.py")


def fixture(root: Path, *, wildcard: str, sans: str) -> None:
    """One domain and one Certificate, with both sides parameterised."""
    tfvars = root / alignment.TFVARS
    tfvars.parent.mkdir(parents=True, exist_ok=True)
    tfvars.write_text(
        "project_id = \"example\"\n"
        "domains = {\n"
        "  studio = {\n"
        '    dns_name    = "example.studio."\n'
        f"    wildcard_certificates = {wildcard}\n"
        "  }\n"
        "}\n",
        encoding="utf-8",
    )

    certificates = root / alignment.CERTIFICATES
    certificates.parent.mkdir(parents=True, exist_ok=True)
    certificates.write_text(
        "---\n"
        "apiVersion: cert-manager.io/v1\n"
        "kind: Certificate\n"
        "metadata:\n"
        "  name: cert-example-studio\n"
        "spec:\n"
        "  secretName: cert-example-studio\n"
        f"{sans}"
        "\n"
        "  duration: 2160h\n",
        encoding="utf-8",
    )


def run(**kwargs) -> list[str]:
    with tempfile.TemporaryDirectory() as directory:
        root = Path(directory)
        fixture(root, **kwargs)
        return alignment.check(root)


APEX_ONLY = '  dnsNames:\n    - "example.studio"\n'
WILDCARD = '  dnsNames:\n    - "*.example.studio"\n'


def test_apex_only_certificate_with_wildcards_forbidden_is_the_aligned_case():
    assert run(wildcard="false", sans=APEX_ONLY) == []


def test_wildcard_certificate_with_wildcards_permitted_is_aligned():
    assert run(wildcard="true", sans=WILDCARD) == []


def test_wildcard_requested_but_caa_forbids_it_is_rejected():
    """The loud-but-late failure: issuance is refused, possibly 60 days later."""
    errors = run(wildcard="false", sans=WILDCARD)
    assert len(errors) == 1
    assert "REFUSE" in errors[0]
    assert "*.example.studio" in errors[0]


def test_wildcard_permitted_but_never_requested_is_rejected():
    """The silent failure: a standing authorisation nothing ever trips over."""
    errors = run(wildcard="true", sans=APEX_ONLY)
    assert len(errors) == 1
    assert "standing authorisation" in errors[0]


def test_certificate_for_a_domain_with_no_zone_is_rejected():
    errors = run(wildcard="false", sans='  dnsNames:\n    - "elsewhere.test"\n')
    assert any("no zone" in error for error in errors)


def test_a_blank_line_above_dnsNames_does_not_hide_the_sans():
    """Regression: a greedy ^(\\s*) anchored a line early and parsed no SANs,
    which silently turned every wildcard case into 'declares no dnsNames'."""
    errors = run(wildcard="false", sans='\n  dnsNames:\n    - "*.example.studio"\n')
    assert len(errors) == 1
    assert "REFUSE" in errors[0], errors


def test_omitted_wildcard_certificates_uses_the_terraform_default():
    """variables.tf defaults the field to true; the check must agree, or an
    omitted field is judged against a policy Terraform will not publish."""
    with tempfile.TemporaryDirectory() as directory:
        root = Path(directory)
        fixture(root, wildcard="true", sans=WILDCARD)
        tfvars = root / alignment.TFVARS
        tfvars.write_text(
            tfvars.read_text().replace("    wildcard_certificates = true\n", ""),
            encoding="utf-8",
        )
        assert alignment.check(root) == []


def test_the_real_repository_is_aligned():
    assert alignment.check(ROOT) == []

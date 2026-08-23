#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Verify that canonical system-design claims match source and maturity metadata."""

from __future__ import annotations

import argparse
import re
import tomllib
from pathlib import Path

CANONICAL_LANG = {
    "Go": "fleet control plane and durable global policy",
    "Rust": "online/runtime data plane and node execution",
    "Python": "scientific, model, training, inference, and evaluation numerics",
    "TileLang": "qualified accelerator kernels",
    "TypeScript": "product surfaces and browser/public web clients",
}
REQUIRED_DOCS = (
    "docs/architecture/system-design-reference.md",
    "docs/architecture/system-design-traceability.md",
    "docs/architecture/language-boundaries.md",
    "docs/architecture/control-plane.md",
    "docs/architecture/runtime-data-plane.md",
    "docs/architecture/runtime-authority-and-stage-execution.md",
    "docs/architecture/data-ingestion.md",
    "docs/architecture/preprocessing.md",
    "docs/architecture/msa-and-template-search.md",
    "docs/architecture/training.md",
    "docs/architecture/checkpointing.md",
    "docs/architecture/evaluation.md",
    "docs/architecture/release-evidence.md",
    "docs/architecture/build-and-toolchains.md",
    "docs/architecture/optimization-18-implementation.md",
    "docs/design/decision-register.md",
)
REQUIRED_SOURCE = (
    "libs/go/servicekit",
    "libs/go/coordination/outbox",
    "control/runtime_authority",
    "control/routing",
    "control/registry/reference_databases",
    "control/registry/releases",
    "libs/rust/runtime_core",
    "libs/rust/worker_runtime",
    "libs/rust/ipc",
    "libs/rust/manifests",
    "services/runtime_gateway/src/lib.rs",
    "services/runtime_host/src/lib.rs",
    "preprocessing",
    "components.toml",
    "maturity.toml",
)
# The canonical list of crates retired by the 2026-08 consolidation epoch. `check_rust_workspace`
# and `check_rust_package_manifest` import this set rather than restating it: the same seven names
# previously appeared in three spellings across three modules, so retiring or reinstating a crate
# meant finding every copy, and a missed copy fails open.
REMOVED_COMPAT = frozenset(
    {
        "clock",
        "retry",
        "resource_version",
        "observability",
        "artifact_manifest",
        "byte_spec",
        "python_bindings",
    }
)
REMOVED_COMPAT_CRATE_NAMES = frozenset(f"mindclade_{name}" for name in REMOVED_COMPAT)


def _requirements(go_mod: str) -> list[tuple[str, str]]:
    req = []
    inside = False
    for raw in go_mod.splitlines():
        line = raw.strip()
        if line == "require (":
            inside = True
            continue
        if inside and line == ")":
            inside = False
            continue
        if inside and line and not line.startswith("//"):
            parts = line.split()
            if len(parts) >= 2 and parts[1].startswith("v"):
                req.append((parts[0], parts[1]))
    return req


def check(root: Path) -> list[str]:
    errors: list[str] = []
    for rel in REQUIRED_DOCS + REQUIRED_SOURCE:
        if not (root / rel).exists():
            errors.append(f"required design/source path missing: {rel}")

    system = (root / "docs/architecture/system-design-reference.md").read_text()
    for lang, claim in CANONICAL_LANG.items():
        if lang not in system or claim not in system:
            errors.append(f"system design is missing canonical {lang} authority: {claim}")

    # Key design paths must be traceable from the canonical documents.
    trace = (root / "docs/architecture/system-design-traceability.md").read_text()
    for token in (
        "control/runtime_authority",
        "services/runtime_gateway",
        "services/runtime_host",
        "libs/rust/runtime_core",
        "preprocessing",
        "control/registry/releases",
    ):
        if token not in system and token not in trace:
            errors.append(f"design traceability omits source authority: {token}")

    components = tomllib.loads((root / "components.toml").read_text()).get("component", [])
    by_name = {c.get("name"): c for c in components}
    for name, service in (("runtime.gateway", "runtime_gateway"), ("runtime.host", "runtime_host")):
        c = by_name.get(name)
        if not c:
            errors.append(f"component metadata missing {name}")
            continue
        if c.get("status") != "implemented":
            errors.append(
                f"{name}: source core is implemented but components.toml status is {c.get('status')!r}"
            )
        readme = (root / f"services/{service}/README.md").read_text().lower()
        if "implemented core" not in readme or "production qualification pending" not in readme:
            errors.append(
                f"services/{service}/README.md must distinguish implemented core from pending qualification"
            )

    # Legacy Rust compatibility crates have completed their migration window and
    # are forbidden from reappearing in the canonical implementation graph.
    rust = root / "libs/rust"
    for crate in REMOVED_COMPAT:
        if (rust / crate).exists():
            errors.append(f"libs/rust/{crate}: retired compatibility crate must remain removed")
    legacy_pattern = re.compile("|".join(sorted(REMOVED_COMPAT_CRATE_NAMES)))
    # Documentation and the JSON package manifests are scanned alongside source. Restricting this
    # sweep to *.rs and Cargo.toml let `PACKAGE_MANIFEST.json` keep advertising all seven retired
    # crates as live layer-5 packages long after they were deleted, and a manifest that still
    # names one is precisely where the next stale import gets copied from. Catching it in the
    # declaration is cheaper than catching it after it reaches a crate.
    source_suffixes = {".rs"}
    legacy_sources = (
        list(rust.rglob("*.rs"))
        + list(rust.rglob("Cargo.toml"))
        + list(rust.rglob("*.md"))
        + list(rust.rglob("*.json"))
    )
    for path in sorted(set(legacy_sources)):
        try:
            text = path.read_text()
        except UnicodeDecodeError:
            continue
        if not legacy_pattern.search(text):
            continue
        # A doc or manifest hit is a stale *declaration*, not a build edge. Saying "active Rust
        # code" would send the reader grepping their crates for an import that is not there.
        kind = (
            "active Rust code still depends on"
            if path.suffix in source_suffixes or path.name == "Cargo.toml"
            else "declaration still advertises"
        )
        errors.append(f"{kind} retired compatibility crate: {path.relative_to(root)}")

    # Root go.sum must at least authenticate every direct public requirement and its go.mod.
    go_mod = (root / "go.mod").read_text()
    sum_lines = set((root / "go.sum").read_text().splitlines())
    for mod, ver in _requirements(go_mod):
        prefix = f"{mod} {ver} h1:"
        modprefix = f"{mod} {ver}/go.mod h1:"
        if not any(x.startswith(prefix) for x in sum_lines):
            errors.append(f"go.sum missing module checksum: {mod} {ver}")
        if not any(x.startswith(modprefix) for x in sum_lines):
            errors.append(f"go.sum missing go.mod checksum: {mod} {ver}")

    # Docs must never claim source presence implies qualification.
    for rel in (
        "docs/architecture/system-design-reference.md",
        "QUALIFICATION.md",
        "VALIDATION.md",
    ):
        txt = (root / rel).read_text().lower()
        if "source" in txt and "qualification" not in txt:
            errors.append(f"{rel}: status language must distinguish source from qualification")
    return errors


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--repo", type=Path, default=Path(__file__).resolve().parents[2])
    root = ap.parse_args().repo.resolve()
    errors = check(root)
    for e in errors:
        print(e)
    print(
        "code/docs alignment check passed"
        if not errors
        else f"code/docs alignment check failed: {len(errors)}"
    )
    return 1 if errors else 0


if __name__ == "__main__":
    raise SystemExit(main())

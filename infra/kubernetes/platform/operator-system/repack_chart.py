#!/usr/bin/env python3
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

"""Repack locked operator charts with Mindclade's transactional phase controls.

The input must be the unmodified upstream chart archive whose SHA-256 is supplied
on the command line. The output is deterministic: archive members are sorted,
timestamps and ownership are cleared, and file modes are normalized. This tool
never downloads an artifact and never accepts an unverified input.
"""

from __future__ import annotations

import argparse
import gzip
import hashlib
import subprocess
import tarfile
import tempfile
from pathlib import Path, PurePosixPath

SYNC_OPTIONS = "argocd.argoproj.io/sync-options: Prune=false,Delete=false"
SUPPORTED_CHARTS = ("jobset", "kueue")


def sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as source:
        for chunk in iter(lambda: source.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def safe_extract(archive_path: Path, destination: Path) -> None:
    with tarfile.open(archive_path, "r:gz") as archive:
        for member in archive.getmembers():
            member_path = PurePosixPath(member.name)
            if (
                member_path.is_absolute()
                or ".." in member_path.parts
                or member.issym()
                or member.islnk()
                or member.isdev()
            ):
                raise ValueError(f"unsafe chart archive member: {member.name}")
        archive.extractall(destination, filter="data")


def replace_once(path: Path, old: str, new: str) -> None:
    text = path.read_text(encoding="utf-8")
    if text.count(old) != 1:
        raise ValueError(f"expected one phase-control anchor in {path}: {old!r}")
    path.write_text(text.replace(old, new, 1), encoding="utf-8")


def add_phase_values(chart: str, chart_root: Path) -> None:
    values_path = chart_root / "values.yaml"
    if chart == "jobset":
        replace_once(
            values_path,
            "controller:\n",
            "controller:\n"
            "  # Mindclade downstream install phase. CRD-only renders set this false.\n"
            "  enabled: true\n",
        )
        return

    phase_values = (
        "# Mindclade downstream transactional install controls. The full-chart defaults stay\n"
        "# compatible with upstream; GitOps always selects exactly one phase.\n"
        "controller:\n"
        "  enabled: true\n"
    )
    phase_values += "crds:\n  enabled: true\n"
    values_path.write_text(
        phase_values + "\n" + values_path.read_text(encoding="utf-8"),
        encoding="utf-8",
    )


def controller_templates(chart: str, chart_root: Path) -> list[Path]:
    templates = []
    for path in sorted((chart_root / "templates").rglob("*.yaml")):
        relative = path.relative_to(chart_root).as_posix()
        if chart == "kueue" and relative.startswith("templates/crd/"):
            continue
        templates.append(path)
    if not templates:
        raise ValueError(f"{chart}: no controller templates selected")
    return templates


def crd_templates(chart: str, chart_root: Path) -> list[Path]:
    if chart == "jobset":
        paths = sorted((chart_root / "crds").glob("*.yaml"))
    elif chart == "kueue":
        paths = sorted((chart_root / "templates" / "crd").glob("*.yaml"))
    else:
        paths = sorted((chart_root / "templates" / "crd").glob("*.yaml"))
    if not paths:
        raise ValueError(f"{chart}: no CRD templates selected")
    return paths


def wrap_controller_templates(chart: str, chart_root: Path) -> None:
    opening = "{{- if .Values.controller.enabled }}\n"
    closing = "{{- end }}\n"
    for path in controller_templates(chart, chart_root):
        text = path.read_text(encoding="utf-8")
        if text.startswith(opening):
            raise ValueError(f"{path} is already phase wrapped")
        path.write_text(opening + text.rstrip() + "\n" + closing, encoding="utf-8")


def protect_crds(chart: str, chart_root: Path) -> None:
    for path in crd_templates(chart, chart_root):
        text = path.read_text(encoding="utf-8")
        if SYNC_OPTIONS in text:
            raise ValueError(f"{path} already contains Argo CRD lifecycle protection")
        anchor = "  annotations:\n"
        if anchor not in text:
            raise ValueError(f"{path}: expected a metadata annotations block")
        path.write_text(
            text.replace(anchor, anchor + f"    {SYNC_OPTIONS}\n", 1),
            encoding="utf-8",
        )


def wrap_kueue_crds(chart: str, chart_root: Path) -> None:
    if chart != "kueue":
        return
    opening = "{{- if .Values.crds.enabled }}\n"
    closing = "{{- end }}\n"
    for path in crd_templates(chart, chart_root):
        text = path.read_text(encoding="utf-8")
        path.write_text(opening + text.rstrip() + "\n" + closing, encoding="utf-8")


def apply_patch_file(extraction_root: Path, patch_path: Path) -> None:
    subprocess.run(
        ["patch", "--batch", "--forward", "-p1", "-i", str(patch_path.resolve())],
        cwd=extraction_root,
        check=True,
    )


def deterministic_archive(chart_root: Path, output_path: Path) -> None:
    output_path.parent.mkdir(parents=True, exist_ok=True)
    with (
        output_path.open("wb") as raw_output,
        gzip.GzipFile(filename="", mode="wb", fileobj=raw_output, mtime=0) as compressed,
        tarfile.open(fileobj=compressed, mode="w", format=tarfile.PAX_FORMAT) as archive,
    ):
        for path in sorted(
            chart_root.parent.rglob("*"),
            key=lambda item: item.relative_to(chart_root.parent).as_posix(),
        ):
            relative = path.relative_to(chart_root.parent).as_posix()
            info = tarfile.TarInfo(relative)
            info.uid = 0
            info.gid = 0
            info.uname = ""
            info.gname = ""
            info.mtime = 0
            if path.is_dir():
                info.type = tarfile.DIRTYPE
                info.mode = 0o755
                archive.addfile(info)
            elif path.is_file():
                info.type = tarfile.REGTYPE
                info.mode = 0o644
                info.size = path.stat().st_size
                with path.open("rb") as source:
                    archive.addfile(info, source)
            else:
                raise ValueError(f"unsupported extracted chart member: {path}")


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument("--chart", required=True, choices=SUPPORTED_CHARTS)
    parser.add_argument("--source", required=True, type=Path)
    parser.add_argument("--expected-sha256", required=True)
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--patch", action="append", default=[], type=Path)
    return parser.parse_args()


def main() -> None:
    args = parse_args()
    if not args.expected_sha256 or len(args.expected_sha256) != 64:
        raise ValueError("--expected-sha256 must be exactly 64 lowercase hexadecimal characters")
    actual_sha = sha256(args.source)
    if actual_sha != args.expected_sha256:
        raise ValueError(
            f"upstream archive digest mismatch: expected {args.expected_sha256}, got {actual_sha}"
        )

    with tempfile.TemporaryDirectory(prefix=f"mindclade-{args.chart}-chart-") as temp_dir:
        extraction_root = Path(temp_dir)
        safe_extract(args.source, extraction_root)
        chart_root = extraction_root / args.chart
        if not (chart_root / "Chart.yaml").is_file():
            raise ValueError(f"archive does not contain {args.chart}/Chart.yaml")
        for patch_path in args.patch:
            apply_patch_file(extraction_root, patch_path)
        add_phase_values(args.chart, chart_root)
        protect_crds(args.chart, chart_root)
        wrap_controller_templates(args.chart, chart_root)
        wrap_kueue_crds(args.chart, chart_root)
        deterministic_archive(chart_root, args.output)

    print(f"sha256:{sha256(args.output)}  {args.output}")


if __name__ == "__main__":
    main()

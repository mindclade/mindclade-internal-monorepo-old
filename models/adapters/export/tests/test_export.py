# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Round-trip, parity, state-independence, and hostile-file export tests."""

from __future__ import annotations

import hashlib
import json
import os
import shutil
import subprocess
import sys
import textwrap
from dataclasses import FrozenInstanceError
from pathlib import Path
from typing import Any

import pytest
import torch
from torch import nn

from models.adapters.export import (
    EXPORT_MANIFEST_FILENAME,
    EXPORT_MANIFEST_SCHEMA_VERSION,
    EXPORT_USAGE,
    EXPORTED_PROGRAM_FILENAME,
    DynamicDimension,
    ExportManifest,
    LoadedExportBundle,
    TensorInputContract,
    export_bundle,
    load_export_bundle,
    validate_export_parity,
)

CONFIGURATION_SHA256 = "sha256:" + "a" * 64
SOURCE_SHA256 = "sha256:" + "b" * 64
RUNTIME_SHA256 = "sha256:" + "c" * 64
KERNEL_MANIFEST_SHA256 = "sha256:" + "d" * 64
RTOL = 1e-5
ATOL = 1e-6


class TinyExportModel(nn.Module):
    def __init__(self) -> None:
        super().__init__()
        self.projection = nn.Linear(4, 3)

    def forward(self, features: torch.Tensor) -> torch.Tensor:
        return torch.tanh(self.projection(features))


def _contract(example: torch.Tensor) -> TensorInputContract:
    return TensorInputContract.from_tensor(
        "features",
        example,
        dynamic_dimensions=(
            DynamicDimension(axis=0, name="batch", minimum=1, maximum=4),
            DynamicDimension(axis=1, name="tokens", minimum=2, maximum=6),
        ),
    )


def _export(
    destination: Path,
    *,
    model: TinyExportModel | None = None,
    example: torch.Tensor | None = None,
) -> tuple[ExportManifest, TinyExportModel, torch.Tensor]:
    selected_model = model if model is not None else TinyExportModel().eval()
    selected_example = example if example is not None else torch.randn(2, 3, 4)
    manifest = export_bundle(
        selected_model,
        (selected_example,),
        (_contract(selected_example),),
        destination,
        configuration_sha256=CONFIGURATION_SHA256,
        source_sha256=SOURCE_SHA256,
        runtime_sha256=RUNTIME_SHA256,
        kernel_manifest_sha256=KERNEL_MANIFEST_SHA256,
    )
    return manifest, selected_model, selected_example


@pytest.fixture(scope="module")
def exported_template(
    tmp_path_factory: pytest.TempPathFactory,
) -> tuple[Path, ExportManifest, torch.Tensor]:
    torch.manual_seed(101)
    destination = tmp_path_factory.mktemp("export-template") / "bundle"
    manifest, model, example = _export(destination)
    with torch.inference_mode():
        expected = model(example).detach().clone()
    return destination, manifest, expected


def _copy_bundle(template: Path, tmp_path: Path) -> Path:
    destination = tmp_path / "bundle"
    shutil.copytree(template, destination)
    return destination


def _sha256(content: bytes) -> str:
    return f"sha256:{hashlib.sha256(content).hexdigest()}"


def _load(
    path: Path,
    manifest_sha256: str,
    *,
    runtime_sha256: str = RUNTIME_SHA256,
    kernel_manifest_sha256: str = KERNEL_MANIFEST_SHA256,
    **kwargs: Any,
) -> LoadedExportBundle:
    return load_export_bundle(
        path,
        expected_manifest_sha256=manifest_sha256,
        expected_runtime_sha256=runtime_sha256,
        expected_kernel_manifest_sha256=kernel_manifest_sha256,
        **kwargs,
    )


def _canonical_document(document: dict[str, Any]) -> bytes:
    return json.dumps(
        document,
        sort_keys=True,
        separators=(",", ":"),
        ensure_ascii=False,
        allow_nan=False,
    ).encode("utf-8")


def test_round_trip_manifest_and_dynamic_boundary_parity(
    exported_template: tuple[Path, ExportManifest, torch.Tensor],
) -> None:
    path, manifest, _ = exported_template
    assert set(item.name for item in path.iterdir()) == {
        EXPORTED_PROGRAM_FILENAME,
        EXPORT_MANIFEST_FILENAME,
    }
    assert (path / EXPORT_MANIFEST_FILENAME).read_bytes() == manifest.canonical_bytes()
    assert manifest.schema_version == EXPORT_MANIFEST_SCHEMA_VERSION
    assert manifest.configuration_sha256 == CONFIGURATION_SHA256
    assert manifest.source_sha256 == SOURCE_SHA256
    assert manifest.runtime_sha256 == RUNTIME_SHA256
    assert manifest.kernel_manifest_sha256 == KERNEL_MANIFEST_SHA256
    assert manifest.usage == EXPORT_USAGE

    loaded = _load(path, manifest.sha256)
    assert loaded.manifest == manifest
    factory_calls: list[TinyExportModel] = []

    def factory() -> TinyExportModel:
        result = TinyExportModel()
        factory_calls.append(result)
        return result

    minimum = torch.randn(1, 2, 4)
    maximum = torch.randn(4, 6, 4)
    report = validate_export_parity(
        loaded,
        factory,
        ((minimum,), (maximum,)),
        rtol=RTOL,
        atol=ATOL,
    )
    assert report.case_count == 2
    assert report.case_shapes == ((((1, 2, 4),)), (((4, 6, 4),)))
    assert len(factory_calls) == 1


def test_manifest_and_nested_contracts_are_immutable(
    exported_template: tuple[Path, ExportManifest, torch.Tensor],
) -> None:
    _, manifest, _ = exported_template
    with pytest.raises(FrozenInstanceError):
        manifest.schema_version = 2  # type: ignore[misc]
    with pytest.raises(FrozenInstanceError):
        manifest.input_contracts[0].shape = (9,)  # type: ignore[misc]
    with pytest.raises(FrozenInstanceError):
        manifest.input_contracts[0].dynamic_dimensions[0].maximum = 99  # type: ignore[misc]


def test_input_contract_rejects_noncontiguous_strided_tensors() -> None:
    example = torch.randn(2, 3, 4)
    contract = _contract(example)
    noncontiguous = torch.randn(2, 4, 3).transpose(1, 2)
    assert noncontiguous.shape == example.shape
    assert not noncontiguous.is_contiguous()
    with pytest.raises(ValueError, match="contiguous"):
        contract.validate_tensor(noncontiguous)
    with pytest.raises(ValueError, match="contiguous"):
        TensorInputContract.from_tensor("features", noncontiguous)


def test_saved_program_state_is_independent_of_original_module(tmp_path: Path) -> None:
    torch.manual_seed(103)
    model = TinyExportModel().eval()
    example = torch.randn(2, 3, 4)
    with torch.inference_mode():
        expected = model(example).detach().clone()
    manifest, _, _ = _export(tmp_path / "bundle", model=model, example=example)
    with torch.no_grad():
        for parameter in model.parameters():
            parameter.add_(100.0)
    loaded = _load(tmp_path / "bundle", manifest.sha256)
    with torch.inference_mode():
        actual = loaded.program.module()(example)
        mutated = model(example)
    torch.testing.assert_close(actual, expected, rtol=RTOL, atol=ATOL)
    with pytest.raises(AssertionError):
        torch.testing.assert_close(actual, mutated, rtol=RTOL, atol=ATOL)


def test_tampered_artifact_rejected_before_deserialization(
    exported_template: tuple[Path, ExportManifest, torch.Tensor],
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    template, manifest, _ = exported_template
    path = _copy_bundle(template, tmp_path)
    artifact = path / EXPORTED_PROGRAM_FILENAME
    content = bytearray(artifact.read_bytes())
    content[len(content) // 2] ^= 0x01
    artifact.write_bytes(content)
    monkeypatch.setattr(
        torch.export,
        "load",
        lambda *_args, **_kwargs: pytest.fail("unverified artifact was deserialized"),
    )
    with pytest.raises(ValueError, match="checksum"):
        _load(path, manifest.sha256)


def test_untrusted_manifest_identity_rejected_before_deserialization(
    exported_template: tuple[Path, ExportManifest, torch.Tensor],
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    path, _, _ = exported_template
    monkeypatch.setattr(
        torch.export,
        "load",
        lambda *_args, **_kwargs: pytest.fail("untrusted artifact was deserialized"),
    )
    with pytest.raises(ValueError, match="trusted SHA-256"):
        _load(path, "sha256:" + "0" * 64)


@pytest.mark.parametrize(
    "keyword",
    ["runtime_sha256", "kernel_manifest_sha256"],
)
def test_active_runtime_identities_are_required_before_deserialization(
    exported_template: tuple[Path, ExportManifest, torch.Tensor],
    monkeypatch: pytest.MonkeyPatch,
    keyword: str,
) -> None:
    path, manifest, _ = exported_template
    monkeypatch.setattr(
        torch.export,
        "load",
        lambda *_args, **_kwargs: pytest.fail("incompatible artifact was deserialized"),
    )
    with pytest.raises(ValueError, match="identity"):
        _load(path, manifest.sha256, **{keyword: "sha256:" + "0" * 64})


def test_pytorch_runtime_version_is_checked_before_deserialization(
    exported_template: tuple[Path, ExportManifest, torch.Tensor],
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    template, _, _ = exported_template
    path = _copy_bundle(template, tmp_path)
    manifest_path = path / EXPORT_MANIFEST_FILENAME
    document = json.loads(manifest_path.read_text(encoding="utf-8"))
    document["torch_version"] = "0.0.0-incompatible"
    content = _canonical_document(document)
    manifest_path.write_bytes(content)
    monkeypatch.setattr(
        torch.export,
        "load",
        lambda *_args, **_kwargs: pytest.fail("incompatible artifact was deserialized"),
    )
    with pytest.raises(ValueError, match="PyTorch version"):
        _load(path, _sha256(content))


def test_bundle_and_artifact_symlinks_are_rejected(
    exported_template: tuple[Path, ExportManifest, torch.Tensor], tmp_path: Path
) -> None:
    template, manifest, _ = exported_template
    alias = tmp_path / "bundle-alias"
    alias.symlink_to(template, target_is_directory=True)
    with pytest.raises(ValueError, match="non-symlink directory"):
        _load(alias, manifest.sha256)

    copied = _copy_bundle(template, tmp_path / "copied")
    artifact = copied / EXPORTED_PROGRAM_FILENAME
    outside = tmp_path / "outside.pt2"
    artifact.replace(outside)
    artifact.symlink_to(outside)
    with pytest.raises(ValueError, match="opened safely"):
        _load(copied, manifest.sha256)


def test_non_regular_artifact_is_rejected(
    exported_template: tuple[Path, ExportManifest, torch.Tensor], tmp_path: Path
) -> None:
    template, manifest, _ = exported_template
    path = _copy_bundle(template, tmp_path)
    artifact = path / EXPORTED_PROGRAM_FILENAME
    artifact.unlink()
    artifact.mkdir()
    with pytest.raises(ValueError, match="regular"):
        _load(path, manifest.sha256)


def test_caller_size_cap_rejected_before_deserialization(
    exported_template: tuple[Path, ExportManifest, torch.Tensor],
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    path, manifest, _ = exported_template
    monkeypatch.setattr(
        torch.export,
        "load",
        lambda *_args, **_kwargs: pytest.fail("oversized artifact was deserialized"),
    )
    with pytest.raises(ValueError, match="caller byte limit"):
        _load(
            path,
            manifest.sha256,
            maximum_artifact_bytes=manifest.artifact_size_bytes - 1,
        )


@pytest.mark.parametrize("mutation", ["schema", "schema_float", "missing", "unknown"])
def test_manifest_schema_and_field_set_are_strict(
    exported_template: tuple[Path, ExportManifest, torch.Tensor],
    tmp_path: Path,
    mutation: str,
) -> None:
    template, _, _ = exported_template
    path = _copy_bundle(template, tmp_path)
    manifest_path = path / EXPORT_MANIFEST_FILENAME
    document = json.loads(manifest_path.read_text(encoding="utf-8"))
    assert isinstance(document, dict)
    if mutation == "schema":
        document["schema_version"] = EXPORT_MANIFEST_SCHEMA_VERSION + 1
        message = "schema_version"
    elif mutation == "schema_float":
        document["schema_version"] = 1.0
        message = "schema_version"
    elif mutation == "missing":
        document.pop("runtime_sha256")
        message = "unknown or missing"
    else:
        document["unexpected"] = "field"
        message = "unknown or missing"
    content = _canonical_document(document)
    manifest_path.write_bytes(content)
    with pytest.raises(ValueError, match=message):
        _load(path, _sha256(content))


def test_duplicate_manifest_key_is_rejected(
    exported_template: tuple[Path, ExportManifest, torch.Tensor], tmp_path: Path
) -> None:
    template, _, _ = exported_template
    path = _copy_bundle(template, tmp_path)
    manifest_path = path / EXPORT_MANIFEST_FILENAME
    original = manifest_path.read_bytes()
    duplicate = b'{"schema_version":1,' + original[1:]
    manifest_path.write_bytes(duplicate)
    with pytest.raises(ValueError, match="duplicate"):
        _load(path, _sha256(duplicate))


def test_excessively_nested_manifest_fails_closed_before_deserialization(
    exported_template: tuple[Path, ExportManifest, torch.Tensor],
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    template, _, _ = exported_template
    path = _copy_bundle(template, tmp_path)
    manifest_path = path / EXPORT_MANIFEST_FILENAME
    original = manifest_path.read_bytes()

    def recursive_decoder(*_args: object, **_kwargs: object) -> object:
        raise RecursionError("injected excessive manifest nesting")

    monkeypatch.setattr(json, "loads", recursive_decoder)
    monkeypatch.setattr(
        torch.export,
        "load",
        lambda *_args, **_kwargs: pytest.fail("hostile manifest reached deserialization"),
    )
    with pytest.raises(ValueError, match="valid UTF-8 JSON"):
        _load(path, _sha256(original))


def test_manifest_cannot_claim_deployment_authority(
    exported_template: tuple[Path, ExportManifest, torch.Tensor],
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    template, _, _ = exported_template
    path = _copy_bundle(template, tmp_path)
    manifest_path = path / EXPORT_MANIFEST_FILENAME
    document = json.loads(manifest_path.read_text(encoding="utf-8"))
    document["usage"] = "deployment"
    content = _canonical_document(document)
    manifest_path.write_bytes(content)
    monkeypatch.setattr(
        torch.export,
        "load",
        lambda *_args, **_kwargs: pytest.fail("deployment claim reached deserialization"),
    )
    with pytest.raises(ValueError, match="not authorized for deployment"):
        _load(path, _sha256(content))


def test_fresh_process_load_and_parity(
    exported_template: tuple[Path, ExportManifest, torch.Tensor],
) -> None:
    path, manifest, _ = exported_template
    child = textwrap.dedent(
        """
        import pathlib
        import sys

        import torch
        from torch import nn

        from models.adapters.export import load_export_bundle, validate_export_parity


        class FreshTinyExportModel(nn.Module):
            def __init__(self) -> None:
                super().__init__()
                self.projection = nn.Linear(4, 3)

            def forward(self, features: torch.Tensor) -> torch.Tensor:
                return torch.tanh(self.projection(features))


        bundle = load_export_bundle(
            pathlib.Path(sys.argv[1]),
            expected_manifest_sha256=sys.argv[2],
            expected_runtime_sha256=sys.argv[3],
            expected_kernel_manifest_sha256=sys.argv[4],
        )
        minimum = torch.arange(8, dtype=torch.float32).reshape(1, 2, 4)
        maximum = torch.arange(96, dtype=torch.float32).reshape(4, 6, 4)
        report = validate_export_parity(
            bundle,
            FreshTinyExportModel,
            ((minimum,), (maximum,)),
            rtol=1e-5,
            atol=1e-6,
        )
        if report.case_count != 2:
            raise SystemExit("fresh-process parity did not validate both boundary cases")
        """
    )
    completed = subprocess.run(
        [
            sys.executable,
            "-c",
            child,
            str(path),
            manifest.sha256,
            RUNTIME_SHA256,
            KERNEL_MANIFEST_SHA256,
        ],
        check=False,
        capture_output=True,
        env={**os.environ, "PYTHONPATH": os.pathsep.join(sys.path)},
        text=True,
        timeout=120,
    )
    assert completed.returncode == 0, completed.stderr


def test_contract_and_tolerance_failures_are_explicit(
    exported_template: tuple[Path, ExportManifest, torch.Tensor], tmp_path: Path
) -> None:
    path, manifest, _ = exported_template
    loaded = _load(path, manifest.sha256)
    with pytest.raises(ValueError, match="outside its dynamic range"):
        validate_export_parity(loaded, TinyExportModel, ((torch.randn(5, 3, 4),),))
    with pytest.raises(TypeError, match="dtype"):
        manifest.input_contracts[0].validate_tensor(torch.randn(2, 3, 4).double())
    for tolerance in (float("nan"), float("inf"), -1.0):
        with pytest.raises(ValueError, match="finite and non-negative"):
            validate_export_parity(
                loaded,
                TinyExportModel,
                ((torch.randn(2, 3, 4),),),
                rtol=tolerance,
            )

    double_model = TinyExportModel().double().eval()
    double_example = torch.randn(2, 3, 4, dtype=torch.float64)
    double_manifest, _, _ = _export(
        tmp_path / "double-bundle",
        model=double_model,
        example=double_example,
    )
    double_loaded = _load(tmp_path / "double-bundle", double_manifest.sha256)
    with pytest.raises(TypeError, match=r"state.*dtype"):
        validate_export_parity(
            double_loaded,
            TinyExportModel,
            ((double_example,),),
        )

    training_model = TinyExportModel().train()
    example = torch.randn(2, 3, 4)
    with pytest.raises(ValueError, match="eval mode"):
        export_bundle(
            training_model,
            (example,),
            (_contract(example),),
            tmp_path / "training",
            configuration_sha256=CONFIGURATION_SHA256,
            source_sha256=SOURCE_SHA256,
            runtime_sha256=RUNTIME_SHA256,
            kernel_manifest_sha256=KERNEL_MANIFEST_SHA256,
        )


def test_atomic_save_failure_leaves_no_destination_or_temporary_directory(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    destination = tmp_path / "failed-bundle"
    example = torch.randn(2, 3, 4)

    def fail_save(*_args: object, **_kwargs: object) -> None:
        raise RuntimeError("injected save failure")

    monkeypatch.setattr(torch.export, "save", fail_save)
    with pytest.raises(RuntimeError, match="injected save failure"):
        export_bundle(
            TinyExportModel().eval(),
            (example,),
            (_contract(example),),
            destination,
            configuration_sha256=CONFIGURATION_SHA256,
            source_sha256=SOURCE_SHA256,
            runtime_sha256=RUNTIME_SHA256,
            kernel_manifest_sha256=KERNEL_MANIFEST_SHA256,
        )
    assert not destination.exists()
    assert list(tmp_path.glob(f".{destination.name}.tmp-*")) == []


def test_existing_destination_is_not_overwritten(
    exported_template: tuple[Path, ExportManifest, torch.Tensor],
) -> None:
    path, manifest, _ = exported_template
    original = (path / EXPORT_MANIFEST_FILENAME).read_bytes()
    example = torch.randn(2, 3, 4)
    with pytest.raises(FileExistsError, match="already exists"):
        export_bundle(
            TinyExportModel().eval(),
            (example,),
            (_contract(example),),
            path,
            configuration_sha256=CONFIGURATION_SHA256,
            source_sha256=SOURCE_SHA256,
            runtime_sha256=RUNTIME_SHA256,
            kernel_manifest_sha256=KERNEL_MANIFEST_SHA256,
        )
    assert (path / EXPORT_MANIFEST_FILENAME).read_bytes() == original
    assert manifest.sha256 == _sha256(original)


def test_destination_created_during_capture_is_not_overwritten(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    destination = tmp_path / "raced-bundle"
    example = torch.randn(2, 3, 4)
    original_save = torch.export.save

    def save_then_create_destination(*args: Any, **kwargs: Any) -> None:
        original_save(*args, **kwargs)
        destination.mkdir()
        (destination / "owner-data").write_text("preserve")

    monkeypatch.setattr(torch.export, "save", save_then_create_destination)
    with pytest.raises(FileExistsError, match="already exists"):
        export_bundle(
            TinyExportModel().eval(),
            (example,),
            (_contract(example),),
            destination,
            configuration_sha256=CONFIGURATION_SHA256,
            source_sha256=SOURCE_SHA256,
            runtime_sha256=RUNTIME_SHA256,
            kernel_manifest_sha256=KERNEL_MANIFEST_SHA256,
        )
    assert (destination / "owner-data").read_text() == "preserve"
    assert list(tmp_path.glob(f".{destination.name}.tmp-*")) == []

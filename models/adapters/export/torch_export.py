# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Bounded, content-verified local ``torch.export`` bundles."""

from __future__ import annotations

import hashlib
import hmac
import io
import json
import os
import re
import shutil
import stat
import tempfile
from collections.abc import Mapping, Sequence
from dataclasses import dataclass
from pathlib import Path
from typing import Final, cast

import torch
from torch import nn

from libs.python.serialization import canonical_json_bytes

EXPORT_MANIFEST_SCHEMA_VERSION: Final = 1
EXPORT_FORMAT: Final = "torch.export.pt2"
EXPORT_USAGE: Final = "source-reference-only"
EXPORTED_PROGRAM_FILENAME: Final = "program.pt2"
EXPORT_MANIFEST_FILENAME: Final = "manifest.json"
MAXIMUM_EXPORTED_PROGRAM_BYTES: Final = 512 << 20
MAXIMUM_EXPORT_MANIFEST_BYTES: Final = 1 << 20
MAXIMUM_INPUTS: Final = 64
MAXIMUM_RANK: Final = 16
MAXIMUM_DIMENSION_SIZE: Final = (1 << 31) - 1
MAXIMUM_NAME_LENGTH: Final = 128
_READ_CHUNK_BYTES: Final = 1 << 20
_SHA256 = re.compile(r"^sha256:[0-9a-f]{64}$")
_NAME = re.compile(r"^[A-Za-z][A-Za-z0-9_]{0,127}$")
_DTYPES: Final[dict[str, torch.dtype]] = {
    "bfloat16": torch.bfloat16,
    "bool": torch.bool,
    "float16": torch.float16,
    "float32": torch.float32,
    "float64": torch.float64,
    "int8": torch.int8,
    "int16": torch.int16,
    "int32": torch.int32,
    "int64": torch.int64,
    "uint8": torch.uint8,
}
_DIMENSION_FIELDS: Final = frozenset({"axis", "name", "minimum", "maximum"})
_INPUT_FIELDS: Final = frozenset(
    {
        "name",
        "dtype",
        "shape",
        "device",
        "layout",
        "requires_grad",
        "dynamic_dimensions",
    }
)
_MANIFEST_FIELDS: Final = frozenset(
    {
        "schema_version",
        "format",
        "artifact_filename",
        "artifact_sha256",
        "artifact_size_bytes",
        "configuration_sha256",
        "source_sha256",
        "runtime_sha256",
        "kernel_manifest_sha256",
        "torch_version",
        "input_contracts",
        "usage",
    }
)


@dataclass(frozen=True, slots=True)
class DynamicDimension:
    """One named, bounded dynamic axis in an input tensor."""

    axis: int
    name: str
    minimum: int
    maximum: int

    def __post_init__(self) -> None:
        if isinstance(self.axis, bool) or not isinstance(self.axis, int) or self.axis < 0:
            raise ValueError("dynamic dimension axis must be a non-negative integer")
        if not isinstance(self.name, str) or not _NAME.fullmatch(self.name):
            raise ValueError("dynamic dimension name must be a bounded identifier")
        if any(
            isinstance(value, bool) or not isinstance(value, int)
            for value in (self.minimum, self.maximum)
        ):
            raise ValueError("dynamic dimension bounds must be integers")
        if not 1 <= self.minimum < self.maximum <= MAXIMUM_DIMENSION_SIZE:
            raise ValueError("dynamic dimension bounds must satisfy 1 <= minimum < maximum")

    def to_document(self) -> dict[str, object]:
        return {
            "axis": self.axis,
            "name": self.name,
            "minimum": self.minimum,
            "maximum": self.maximum,
        }

    @classmethod
    def from_document(cls, document: Mapping[str, object]) -> DynamicDimension:
        if not isinstance(document, Mapping) or set(document) != _DIMENSION_FIELDS:
            raise ValueError("dynamic dimension has unknown or missing fields")
        return cls(
            axis=cast(int, document["axis"]),
            name=cast(str, document["name"]),
            minimum=cast(int, document["minimum"]),
            maximum=cast(int, document["maximum"]),
        )


@dataclass(frozen=True, slots=True)
class TensorInputContract:
    """Exact contiguous positional tensor input plus explicitly dynamic axes."""

    name: str
    dtype: str
    shape: tuple[int, ...]
    device: str = "cpu"
    layout: str = "strided"
    requires_grad: bool = False
    dynamic_dimensions: tuple[DynamicDimension, ...] = ()

    def __post_init__(self) -> None:
        if not isinstance(self.name, str) or not _NAME.fullmatch(self.name):
            raise ValueError("input name must be a bounded identifier")
        if self.dtype not in _DTYPES:
            raise ValueError(f"unsupported input dtype {self.dtype!r}")
        try:
            shape = tuple(self.shape)
        except TypeError as error:
            raise TypeError("input shape must be an iterable of integers") from error
        if len(shape) > MAXIMUM_RANK:
            raise ValueError(f"input rank exceeds {MAXIMUM_RANK}")
        if any(
            isinstance(size, bool)
            or not isinstance(size, int)
            or not 1 <= size <= MAXIMUM_DIMENSION_SIZE
            for size in shape
        ):
            raise ValueError("input shape dimensions must be bounded positive integers")
        if not isinstance(self.device, str) or not self.device:
            raise ValueError("input device must be a canonical torch device string")
        try:
            canonical_device = str(torch.device(self.device))
        except (RuntimeError, ValueError) as error:
            raise ValueError("input device must be a canonical torch device string") from error
        if canonical_device != self.device:
            raise ValueError("input device must use its canonical torch spelling")
        if self.layout != "strided":
            raise ValueError("only strided tensor inputs are supported")
        if not isinstance(self.requires_grad, bool):
            raise TypeError("requires_grad must be boolean")
        try:
            dynamic_dimensions = tuple(self.dynamic_dimensions)
        except TypeError as error:
            raise TypeError("dynamic_dimensions must be iterable") from error
        if any(not isinstance(item, DynamicDimension) for item in dynamic_dimensions):
            raise TypeError("dynamic_dimensions must contain DynamicDimension values")
        dynamic_dimensions = tuple(sorted(dynamic_dimensions, key=lambda item: item.axis))
        axes = [item.axis for item in dynamic_dimensions]
        if len(set(axes)) != len(axes):
            raise ValueError("an input axis may have at most one dynamic dimension")
        for dimension in dynamic_dimensions:
            if dimension.axis >= len(shape):
                raise ValueError("dynamic dimension axis is outside the input rank")
            if not dimension.minimum <= shape[dimension.axis] <= dimension.maximum:
                raise ValueError("example input shape lies outside its dynamic range")
        object.__setattr__(self, "shape", shape)
        object.__setattr__(self, "dynamic_dimensions", dynamic_dimensions)

    @classmethod
    def from_tensor(
        cls,
        name: str,
        tensor: torch.Tensor,
        *,
        dynamic_dimensions: Sequence[DynamicDimension] = (),
    ) -> TensorInputContract:
        if not isinstance(tensor, torch.Tensor):
            raise TypeError("input contract source must be a tensor")
        if tensor.layout != torch.strided:
            raise ValueError("only strided tensor inputs are supported")
        if not tensor.is_contiguous():
            raise ValueError("export inputs must be contiguous")
        return cls(
            name=name,
            dtype=_dtype_name(tensor.dtype),
            shape=tuple(tensor.shape),
            device=str(tensor.device),
            requires_grad=tensor.requires_grad,
            dynamic_dimensions=tuple(dynamic_dimensions),
        )

    def validate_tensor(self, tensor: torch.Tensor) -> None:
        if not isinstance(tensor, torch.Tensor):
            raise TypeError(f"input {self.name!r} must be a tensor")
        if _dtype_name(tensor.dtype) != self.dtype:
            raise TypeError(f"input {self.name!r} dtype does not match its contract")
        if str(tensor.device) != self.device:
            raise ValueError(f"input {self.name!r} device does not match its contract")
        if tensor.layout != torch.strided:
            raise ValueError(f"input {self.name!r} must use strided layout")
        if not tensor.is_contiguous():
            raise ValueError(f"input {self.name!r} must be contiguous")
        if tensor.requires_grad != self.requires_grad:
            raise ValueError(f"input {self.name!r} requires_grad does not match its contract")
        actual_shape = tuple(tensor.shape)
        if len(actual_shape) != len(self.shape):
            raise ValueError(f"input {self.name!r} rank does not match its contract")
        dynamic_by_axis = {item.axis: item for item in self.dynamic_dimensions}
        for axis, (actual, example) in enumerate(zip(actual_shape, self.shape, strict=True)):
            dynamic = dynamic_by_axis.get(axis)
            if dynamic is None and actual != example:
                raise ValueError(f"input {self.name!r} static shape does not match its contract")
            if dynamic is not None and not dynamic.minimum <= actual <= dynamic.maximum:
                raise ValueError(f"input {self.name!r} shape is outside its dynamic range")

    def to_document(self) -> dict[str, object]:
        return {
            "name": self.name,
            "dtype": self.dtype,
            "shape": list(self.shape),
            "device": self.device,
            "layout": self.layout,
            "requires_grad": self.requires_grad,
            "dynamic_dimensions": [item.to_document() for item in self.dynamic_dimensions],
        }

    @classmethod
    def from_document(cls, document: Mapping[str, object]) -> TensorInputContract:
        if not isinstance(document, Mapping) or set(document) != _INPUT_FIELDS:
            raise ValueError("input contract has unknown or missing fields")
        shape = document["shape"]
        dynamic_documents = document["dynamic_dimensions"]
        if not _is_sequence(shape) or not _is_sequence(dynamic_documents):
            raise ValueError("input shape and dynamic dimensions must be arrays")
        dimensions: list[DynamicDimension] = []
        for item in cast(Sequence[object], dynamic_documents):
            if not isinstance(item, Mapping):
                raise ValueError("dynamic dimensions must be objects")
            dimensions.append(DynamicDimension.from_document(item))
        return cls(
            name=cast(str, document["name"]),
            dtype=cast(str, document["dtype"]),
            shape=tuple(cast(Sequence[int], shape)),
            device=cast(str, document["device"]),
            layout=cast(str, document["layout"]),
            requires_grad=cast(bool, document["requires_grad"]),
            dynamic_dimensions=tuple(dimensions),
        )


@dataclass(frozen=True, slots=True)
class ExportManifest:
    """Immutable schema-v1 identity and source-reference contract for one PT2 artifact."""

    artifact_sha256: str
    artifact_size_bytes: int
    configuration_sha256: str
    source_sha256: str
    runtime_sha256: str
    kernel_manifest_sha256: str
    torch_version: str
    input_contracts: tuple[TensorInputContract, ...]
    schema_version: int = EXPORT_MANIFEST_SCHEMA_VERSION
    format: str = EXPORT_FORMAT
    artifact_filename: str = EXPORTED_PROGRAM_FILENAME
    usage: str = EXPORT_USAGE

    def __post_init__(self) -> None:
        for field_name in (
            "artifact_sha256",
            "configuration_sha256",
            "source_sha256",
            "runtime_sha256",
            "kernel_manifest_sha256",
        ):
            _validate_sha256(getattr(self, field_name), field_name=field_name)
        if (
            isinstance(self.artifact_size_bytes, bool)
            or not isinstance(self.artifact_size_bytes, int)
            or not 1 <= self.artifact_size_bytes <= MAXIMUM_EXPORTED_PROGRAM_BYTES
        ):
            raise ValueError("artifact_size_bytes is outside the supported bound")
        if (
            not isinstance(self.torch_version, str)
            or not self.torch_version
            or len(self.torch_version) > MAXIMUM_NAME_LENGTH
            or any(ord(character) < 32 for character in self.torch_version)
        ):
            raise ValueError("torch_version must be bounded printable text")
        try:
            contracts = tuple(self.input_contracts)
        except TypeError as error:
            raise TypeError("input_contracts must be iterable") from error
        if not 1 <= len(contracts) <= MAXIMUM_INPUTS:
            raise ValueError(f"input contract count must be in [1, {MAXIMUM_INPUTS}]")
        if any(not isinstance(item, TensorInputContract) for item in contracts):
            raise TypeError("input_contracts must contain TensorInputContract values")
        names = [item.name for item in contracts]
        if len(names) != len(set(names)):
            raise ValueError("input contract names must be unique")
        _validate_shared_dynamic_dimensions(contracts)
        if (
            isinstance(self.schema_version, bool)
            or not isinstance(self.schema_version, int)
            or self.schema_version != EXPORT_MANIFEST_SCHEMA_VERSION
        ):
            raise ValueError(
                f"export manifest schema_version must be {EXPORT_MANIFEST_SCHEMA_VERSION}"
            )
        if self.format != EXPORT_FORMAT:
            raise ValueError(f"export manifest format must be {EXPORT_FORMAT!r}")
        if self.artifact_filename != EXPORTED_PROGRAM_FILENAME:
            raise ValueError("export manifest artifact filename is not supported")
        if self.usage != EXPORT_USAGE:
            raise ValueError("export manifest is not authorized for deployment")
        object.__setattr__(self, "input_contracts", contracts)

    def to_document(self) -> dict[str, object]:
        return {
            "schema_version": self.schema_version,
            "format": self.format,
            "artifact_filename": self.artifact_filename,
            "artifact_sha256": self.artifact_sha256,
            "artifact_size_bytes": self.artifact_size_bytes,
            "configuration_sha256": self.configuration_sha256,
            "source_sha256": self.source_sha256,
            "runtime_sha256": self.runtime_sha256,
            "kernel_manifest_sha256": self.kernel_manifest_sha256,
            "torch_version": self.torch_version,
            "input_contracts": [item.to_document() for item in self.input_contracts],
            "usage": self.usage,
        }

    def canonical_bytes(self) -> bytes:
        return canonical_json_bytes(
            self.to_document(), maximum_encoded_bytes=MAXIMUM_EXPORT_MANIFEST_BYTES
        )

    @property
    def sha256(self) -> str:
        return _sha256_bytes(self.canonical_bytes())

    @classmethod
    def from_document(cls, document: Mapping[str, object]) -> ExportManifest:
        if not isinstance(document, Mapping) or set(document) != _MANIFEST_FIELDS:
            raise ValueError("export manifest has unknown or missing fields")
        contract_documents = document["input_contracts"]
        if not _is_sequence(contract_documents):
            raise ValueError("export manifest input_contracts must be an array")
        contracts: list[TensorInputContract] = []
        for item in cast(Sequence[object], contract_documents):
            if not isinstance(item, Mapping):
                raise ValueError("export manifest input contracts must be objects")
            contracts.append(TensorInputContract.from_document(item))
        return cls(
            artifact_sha256=cast(str, document["artifact_sha256"]),
            artifact_size_bytes=cast(int, document["artifact_size_bytes"]),
            configuration_sha256=cast(str, document["configuration_sha256"]),
            source_sha256=cast(str, document["source_sha256"]),
            runtime_sha256=cast(str, document["runtime_sha256"]),
            kernel_manifest_sha256=cast(str, document["kernel_manifest_sha256"]),
            torch_version=cast(str, document["torch_version"]),
            input_contracts=tuple(contracts),
            schema_version=cast(int, document["schema_version"]),
            format=cast(str, document["format"]),
            artifact_filename=cast(str, document["artifact_filename"]),
            usage=cast(str, document["usage"]),
        )


@dataclass(frozen=True, slots=True)
class LoadedExportBundle:
    """A verified manifest and the exported program loaded from its bytes."""

    manifest: ExportManifest
    program: torch.export.ExportedProgram


def export_bundle(
    module: nn.Module,
    example_inputs: Sequence[torch.Tensor],
    input_contracts: Sequence[TensorInputContract],
    destination: str | os.PathLike[str],
    *,
    configuration_sha256: str,
    source_sha256: str,
    runtime_sha256: str,
    kernel_manifest_sha256: str,
    maximum_artifact_bytes: int = MAXIMUM_EXPORTED_PROGRAM_BYTES,
) -> ExportManifest:
    """Capture and atomically publish a new, non-overwriting local bundle."""
    if not isinstance(module, nn.Module):
        raise TypeError("export module must be an nn.Module")
    if module.training:
        raise ValueError("export module must be in eval mode")
    maximum_artifact_bytes = _validate_artifact_limit(maximum_artifact_bytes)
    for field_name, identity in (
        ("configuration_sha256", configuration_sha256),
        ("source_sha256", source_sha256),
        ("runtime_sha256", runtime_sha256),
        ("kernel_manifest_sha256", kernel_manifest_sha256),
    ):
        _validate_sha256(identity, field_name=field_name)
    inputs = _validated_inputs(example_inputs, input_contracts)
    contracts = tuple(input_contracts)
    path = _validate_new_bundle_destination(destination)
    program = torch.export.export(
        module,
        inputs,
        dynamic_shapes=_dynamic_shapes(contracts),
        strict=True,
    )

    temporary = Path(tempfile.mkdtemp(prefix=f".{path.name}.tmp-", dir=path.parent))
    destination_created = False
    published = False
    try:
        artifact_path = temporary / EXPORTED_PROGRAM_FILENAME
        torch.export.save(program, artifact_path)
        os.chmod(artifact_path, 0o600)
        artifact_sha256, artifact_size = _hash_regular_file(
            artifact_path, maximum_bytes=maximum_artifact_bytes
        )
        manifest = ExportManifest(
            artifact_sha256=artifact_sha256,
            artifact_size_bytes=artifact_size,
            configuration_sha256=configuration_sha256,
            source_sha256=source_sha256,
            runtime_sha256=runtime_sha256,
            kernel_manifest_sha256=kernel_manifest_sha256,
            torch_version=torch.__version__,
            input_contracts=contracts,
        )
        _write_exclusive(temporary / EXPORT_MANIFEST_FILENAME, manifest.canonical_bytes())
        _fsync_directory(temporary)
        try:
            os.mkdir(path, mode=0o700)
        except FileExistsError as error:
            raise FileExistsError(f"export bundle destination already exists: {path}") from error
        destination_created = True
        os.link(
            temporary / EXPORTED_PROGRAM_FILENAME,
            path / EXPORTED_PROGRAM_FILENAME,
            follow_symlinks=False,
        )
        _fsync_directory(path)
        # The manifest is the commit marker. A concurrent reader can only see
        # a fail-closed incomplete directory or the complete verified bundle.
        os.link(
            temporary / EXPORT_MANIFEST_FILENAME,
            path / EXPORT_MANIFEST_FILENAME,
            follow_symlinks=False,
        )
        _fsync_directory(path)
        _fsync_directory(path.parent)
        published = True
        return manifest
    finally:
        _remove_private_temporary_directory(temporary)
        if destination_created and not published:
            _remove_private_temporary_directory(path)


def load_export_bundle(
    source: str | os.PathLike[str],
    *,
    expected_manifest_sha256: str,
    expected_runtime_sha256: str,
    expected_kernel_manifest_sha256: str,
    maximum_artifact_bytes: int = MAXIMUM_EXPORTED_PROGRAM_BYTES,
) -> LoadedExportBundle:
    """Verify a trusted bundle identity before invoking pickle-capable PT2 load.

    All expected identities are mandatory and must come from trusted runtime
    configuration. The manifest identity authenticates the bundle; the runtime,
    kernel-manifest, and exact PyTorch-version checks prevent replay into an
    incompatible execution environment. A checksum stored beside an artifact is
    integrity metadata, not an authenticity signal.
    """
    _validate_sha256(expected_manifest_sha256, field_name="expected_manifest_sha256")
    _validate_sha256(expected_runtime_sha256, field_name="expected_runtime_sha256")
    _validate_sha256(
        expected_kernel_manifest_sha256,
        field_name="expected_kernel_manifest_sha256",
    )
    maximum_artifact_bytes = _validate_artifact_limit(maximum_artifact_bytes)
    directory = _open_bundle_directory(source)
    try:
        entries = _bounded_bundle_entries(directory)
        expected_entries = {EXPORT_MANIFEST_FILENAME, EXPORTED_PROGRAM_FILENAME}
        if entries != expected_entries:
            raise ValueError("export bundle has unknown or missing files")
        manifest_bytes, manifest_sha256 = _read_regular_file_at(
            directory,
            EXPORT_MANIFEST_FILENAME,
            maximum_bytes=MAXIMUM_EXPORT_MANIFEST_BYTES,
        )
        if not hmac.compare_digest(manifest_sha256, expected_manifest_sha256):
            raise ValueError("export manifest does not match the trusted SHA-256 identity")
        manifest = _decode_manifest(manifest_bytes)
        if not hmac.compare_digest(manifest.runtime_sha256, expected_runtime_sha256):
            raise ValueError("export runtime identity does not match the active runtime")
        if not hmac.compare_digest(
            manifest.kernel_manifest_sha256,
            expected_kernel_manifest_sha256,
        ):
            raise ValueError("export kernel manifest identity does not match the active runtime")
        if manifest.torch_version != str(torch.__version__):
            raise ValueError("export PyTorch version does not match the active runtime")
        if manifest.artifact_size_bytes > maximum_artifact_bytes:
            raise ValueError("export artifact exceeds the caller byte limit")
        artifact_bytes, artifact_sha256 = _read_regular_file_at(
            directory,
            manifest.artifact_filename,
            maximum_bytes=maximum_artifact_bytes,
        )
        if len(artifact_bytes) != manifest.artifact_size_bytes:
            raise ValueError("export artifact size does not match its manifest")
        if not hmac.compare_digest(artifact_sha256, manifest.artifact_sha256):
            raise ValueError("export artifact checksum does not match its manifest")
    finally:
        os.close(directory)

    try:
        program = torch.export.load(io.BytesIO(artifact_bytes))
    except Exception as error:
        raise ValueError("verified torch.export artifact could not be loaded") from error
    return LoadedExportBundle(manifest=manifest, program=program)


def _validated_inputs(
    inputs: Sequence[torch.Tensor], contracts: Sequence[TensorInputContract]
) -> tuple[torch.Tensor, ...]:
    if not isinstance(inputs, Sequence):
        raise TypeError("example_inputs must be a flat sequence of tensors")
    if not isinstance(contracts, Sequence):
        raise TypeError("input_contracts must be a sequence")
    normalized_inputs = tuple(inputs)
    normalized_contracts = tuple(contracts)
    if not 1 <= len(normalized_inputs) <= MAXIMUM_INPUTS:
        raise ValueError(f"input count must be in [1, {MAXIMUM_INPUTS}]")
    if len(normalized_inputs) != len(normalized_contracts):
        raise ValueError("input and contract counts must match")
    if any(not isinstance(item, TensorInputContract) for item in normalized_contracts):
        raise TypeError("input_contracts must contain TensorInputContract values")
    names = [item.name for item in normalized_contracts]
    if len(names) != len(set(names)):
        raise ValueError("input contract names must be unique")
    _validate_shared_dynamic_dimensions(normalized_contracts)
    symbol_sizes: dict[str, int] = {}
    for tensor, contract in zip(normalized_inputs, normalized_contracts, strict=True):
        contract.validate_tensor(tensor)
        for dimension in contract.dynamic_dimensions:
            size = tensor.shape[dimension.axis]
            previous = symbol_sizes.setdefault(dimension.name, size)
            if previous != size:
                raise ValueError("inputs sharing a dynamic dimension must have equal sizes")
    return normalized_inputs


def _dynamic_shapes(
    contracts: tuple[TensorInputContract, ...],
) -> tuple[tuple[object, ...], ...] | None:
    if not any(contract.dynamic_dimensions for contract in contracts):
        return None
    symbols: dict[str, object] = {}
    result: list[tuple[object, ...]] = []
    for contract in contracts:
        dimensions: list[object] = [torch.export.Dim.STATIC] * len(contract.shape)
        for dynamic in contract.dynamic_dimensions:
            symbol = symbols.get(dynamic.name)
            if symbol is None:
                symbol = torch.export.Dim(dynamic.name, min=dynamic.minimum, max=dynamic.maximum)
                symbols[dynamic.name] = symbol
            dimensions[dynamic.axis] = symbol
        result.append(tuple(dimensions))
    return tuple(result)


def _validate_shared_dynamic_dimensions(
    contracts: tuple[TensorInputContract, ...],
) -> None:
    definitions: dict[str, tuple[int, int, int]] = {}
    for contract in contracts:
        for dimension in contract.dynamic_dimensions:
            definition = (
                dimension.minimum,
                contract.shape[dimension.axis],
                dimension.maximum,
            )
            previous = definitions.setdefault(dimension.name, definition)
            if previous != definition:
                raise ValueError(
                    "shared dynamic dimensions must have identical bounds and example sizes"
                )


def _decode_manifest(content: bytes) -> ExportManifest:
    def object_without_duplicates(pairs: list[tuple[str, object]]) -> dict[str, object]:
        result: dict[str, object] = {}
        for key, value in pairs:
            if key in result:
                raise ValueError("export manifest contains duplicate object keys")
            result[key] = value
        return result

    def reject_constant(value: str) -> object:
        raise ValueError(f"export manifest contains invalid JSON constant {value}")

    try:
        document = json.loads(
            content.decode("utf-8"),
            object_pairs_hook=object_without_duplicates,
            parse_constant=reject_constant,
        )
    except (UnicodeDecodeError, json.JSONDecodeError, RecursionError) as error:
        raise ValueError("export manifest is not valid UTF-8 JSON") from error
    if not isinstance(document, Mapping):
        raise ValueError("export manifest root must be an object")
    manifest = ExportManifest.from_document(document)
    if manifest.canonical_bytes() != content:
        raise ValueError("export manifest is not canonically encoded")
    return manifest


def _open_bundle_directory(source: str | os.PathLike[str]) -> int:
    path = Path(source)
    try:
        before = os.lstat(path)
    except OSError as error:
        raise ValueError("export bundle cannot be inspected") from error
    if stat.S_ISLNK(before.st_mode) or not stat.S_ISDIR(before.st_mode):
        raise ValueError("export bundle must be a non-symlink directory")
    flags = os.O_RDONLY
    if hasattr(os, "O_DIRECTORY"):
        flags |= os.O_DIRECTORY
    if hasattr(os, "O_NOFOLLOW"):
        flags |= os.O_NOFOLLOW
    try:
        descriptor = os.open(path, flags)
    except OSError as error:
        raise ValueError("export bundle cannot be opened safely") from error
    after = os.fstat(descriptor)
    if not stat.S_ISDIR(after.st_mode) or (before.st_dev, before.st_ino) != (
        after.st_dev,
        after.st_ino,
    ):
        os.close(descriptor)
        raise ValueError("export bundle changed while being opened")
    return descriptor


def _read_regular_file_at(
    directory: int, filename: str, *, maximum_bytes: int
) -> tuple[bytes, str]:
    flags = os.O_RDONLY
    if hasattr(os, "O_NONBLOCK"):
        flags |= os.O_NONBLOCK
    if hasattr(os, "O_NOFOLLOW"):
        flags |= os.O_NOFOLLOW
    try:
        descriptor = os.open(filename, flags, dir_fd=directory)
    except OSError as error:
        raise ValueError(f"export bundle file {filename!r} cannot be opened safely") from error
    try:
        before = os.fstat(descriptor)
        if not stat.S_ISREG(before.st_mode):
            raise ValueError(f"export bundle file {filename!r} must be regular")
        if not 1 <= before.st_size <= maximum_bytes:
            raise ValueError(f"export bundle file {filename!r} size is outside bounds")
        chunks: list[bytes] = []
        hasher = hashlib.sha256()
        remaining = before.st_size
        while remaining:
            chunk = os.read(descriptor, min(remaining, _READ_CHUNK_BYTES))
            if not chunk:
                raise ValueError(f"export bundle file {filename!r} changed while being read")
            chunks.append(chunk)
            hasher.update(chunk)
            remaining -= len(chunk)
        if os.read(descriptor, 1):
            raise ValueError(f"export bundle file {filename!r} changed while being read")
        after = os.fstat(descriptor)
        if (
            after.st_size != before.st_size
            or after.st_mtime_ns != before.st_mtime_ns
            or after.st_ctime_ns != before.st_ctime_ns
        ):
            raise ValueError(f"export bundle file {filename!r} changed while being read")
        return b"".join(chunks), f"sha256:{hasher.hexdigest()}"
    finally:
        os.close(descriptor)


def _bounded_bundle_entries(directory: int) -> set[str]:
    entries: set[str] = set()
    with os.scandir(directory) as iterator:
        for entry in iterator:
            entries.add(entry.name)
            if len(entries) > 2:
                raise ValueError("export bundle has too many files")
    return entries


def _hash_regular_file(path: Path, *, maximum_bytes: int) -> tuple[str, int]:
    flags = os.O_RDONLY
    if hasattr(os, "O_NOFOLLOW"):
        flags |= os.O_NOFOLLOW
    descriptor = os.open(path, flags)
    try:
        file_stat = os.fstat(descriptor)
        if not stat.S_ISREG(file_stat.st_mode):
            raise ValueError("export artifact must be a regular file")
        if not 1 <= file_stat.st_size <= maximum_bytes:
            raise ValueError("export artifact size is outside bounds")
        hasher = hashlib.sha256()
        remaining = file_stat.st_size
        while remaining:
            chunk = os.read(descriptor, min(remaining, _READ_CHUNK_BYTES))
            if not chunk:
                raise ValueError("export artifact changed while being hashed")
            hasher.update(chunk)
            remaining -= len(chunk)
        os.fsync(descriptor)
        return f"sha256:{hasher.hexdigest()}", file_stat.st_size
    finally:
        os.close(descriptor)


def _write_exclusive(path: Path, content: bytes) -> None:
    flags = os.O_WRONLY | os.O_CREAT | os.O_EXCL
    if hasattr(os, "O_NOFOLLOW"):
        flags |= os.O_NOFOLLOW
    descriptor = os.open(path, flags, 0o600)
    try:
        offset = 0
        while offset < len(content):
            written = os.write(descriptor, content[offset:])
            if written <= 0:
                raise OSError("manifest write made no progress")
            offset += written
        os.fsync(descriptor)
    finally:
        os.close(descriptor)


def _validate_new_bundle_destination(destination: str | os.PathLike[str]) -> Path:
    path = Path(destination)
    if not path.name or path.name in {".", ".."}:
        raise ValueError("export bundle destination must name a new directory")
    if os.path.lexists(path):
        raise FileExistsError(f"export bundle destination already exists: {path}")
    try:
        parent_stat = os.lstat(path.parent)
    except OSError as error:
        raise ValueError("export bundle parent must be an existing directory") from error
    if stat.S_ISLNK(parent_stat.st_mode) or not stat.S_ISDIR(parent_stat.st_mode):
        raise ValueError("export bundle parent must be a non-symlink directory")
    return path


def _validate_artifact_limit(value: int) -> int:
    if (
        isinstance(value, bool)
        or not isinstance(value, int)
        or not 1 <= value <= MAXIMUM_EXPORTED_PROGRAM_BYTES
    ):
        raise ValueError(f"maximum_artifact_bytes must be in [1, {MAXIMUM_EXPORTED_PROGRAM_BYTES}]")
    return value


def _validate_sha256(value: object, *, field_name: str) -> None:
    if not isinstance(value, str) or not _SHA256.fullmatch(value):
        raise ValueError(f"{field_name} must be canonical sha256:<64 lowercase hex>")


def _sha256_bytes(content: bytes) -> str:
    return f"sha256:{hashlib.sha256(content).hexdigest()}"


def _dtype_name(dtype: torch.dtype) -> str:
    for name, supported_dtype in _DTYPES.items():
        if dtype == supported_dtype:
            return name
    raise ValueError(f"unsupported input dtype {dtype}")


def _is_sequence(value: object) -> bool:
    return isinstance(value, Sequence) and not isinstance(value, str | bytes | bytearray)


def _fsync_directory(directory: Path) -> None:
    flags = os.O_RDONLY
    if hasattr(os, "O_DIRECTORY"):
        flags |= os.O_DIRECTORY
    descriptor = os.open(directory, flags)
    try:
        os.fsync(descriptor)
    finally:
        os.close(descriptor)


def _remove_private_temporary_directory(path: Path) -> None:
    try:
        path_stat = os.lstat(path)
    except FileNotFoundError:
        return
    if stat.S_ISDIR(path_stat.st_mode) and not stat.S_ISLNK(path_stat.st_mode):
        shutil.rmtree(path)

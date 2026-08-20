# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

"""Composable TOML resolver used by training, inference, evaluation, and preprocessing."""

from __future__ import annotations

import tomllib
from collections.abc import Iterable, Mapping, Sequence
from dataclasses import dataclass
from itertools import islice
from pathlib import Path
from types import MappingProxyType
from typing import Any, Final

from libs.python.errors import InvalidArgument, ResourceExhausted
from libs.python.identifiers import Digest

from .fingerprint import canonical_json, fingerprint
from .merge import deep_merge
from .overrides import MAXIMUM_OVERRIDE_LENGTH, apply_override

MAXIMUM_SOURCE_BYTES: Final = 8 << 20
MAXIMUM_SOURCES: Final = 64
MAXIMUM_OVERLAYS: Final = 64
MAXIMUM_OVERRIDES: Final = 256


@dataclass(frozen=True)
class Source:
    name: str
    path: str
    digest: str

    def __post_init__(self) -> None:
        if (
            not isinstance(self.name, str)
            or not self.name
            or len(self.name) > 255
            or not isinstance(self.path, str)
            or not self.path
            or len(self.path) > 4096
        ):
            raise InvalidArgument(
                "configuration source name and path must be non-empty and bounded",
                reason="configuration_source_identity",
            )
        Digest.parse(self.digest)


@dataclass(frozen=True)
class AppliedOverride:
    expression: str
    path: str

    def __post_init__(self) -> None:
        if (
            not isinstance(self.expression, str)
            or not self.expression
            or len(self.expression) > MAXIMUM_OVERRIDE_LENGTH
            or not isinstance(self.path, str)
            or not self.path
        ):
            raise InvalidArgument(
                "applied override expression and path are required",
                reason="configuration_override_record",
            )


@dataclass(frozen=True)
class ResolvedConfig:
    value: Mapping[str, Any]
    digest: str
    sources: tuple[Source, ...]
    overrides: tuple[AppliedOverride, ...]
    schema_version: int = 1

    def __post_init__(self) -> None:
        frozen = _freeze(self.value)
        expected = fingerprint(frozen)
        if self.digest != expected:
            raise InvalidArgument(
                "resolved configuration digest does not match its value",
                reason="configuration_digest_mismatch",
            )
        if self.schema_version != 1:
            raise InvalidArgument(
                "resolved configuration schema version must be 1",
                reason="configuration_schema_version",
            )
        if isinstance(self.schema_version, bool) or not isinstance(self.schema_version, int):
            raise InvalidArgument(
                "resolved configuration schema version must be an integer",
                reason="configuration_schema_version",
            )
        if len(self.sources) > MAXIMUM_SOURCES or any(
            not isinstance(source, Source) for source in self.sources
        ):
            raise InvalidArgument(
                "resolved configuration sources are invalid or exceed the bound",
                reason="configuration_source_records",
            )
        if len(self.overrides) > MAXIMUM_OVERRIDES or any(
            not isinstance(override, AppliedOverride) for override in self.overrides
        ):
            raise InvalidArgument(
                "resolved configuration overrides are invalid or exceed the bound",
                reason="configuration_override_records",
            )
        object.__setattr__(self, "value", frozen)
        object.__setattr__(self, "sources", tuple(self.sources))
        object.__setattr__(self, "overrides", tuple(self.overrides))

    def canonical_bytes(self) -> bytes:
        return canonical_json(dict(self.value))


def _load(path: Path) -> tuple[dict[str, Any], Source]:
    try:
        with path.open("rb") as source:
            raw = source.read(MAXIMUM_SOURCE_BYTES + 1)
    except OSError as error:
        raise InvalidArgument(
            "configuration source could not be read",
            reason="configuration_source_read",
            fields={"path": str(path)[:4096]},
            cause=error,
        ) from error
    if len(raw) > MAXIMUM_SOURCE_BYTES:
        raise ResourceExhausted(
            f"configuration source exceeds {MAXIMUM_SOURCE_BYTES} bytes",
            reason="configuration_source_size",
            fields={"path": str(path)[:4096]},
        )
    try:
        data = tomllib.loads(raw.decode("utf-8"))
    except (UnicodeDecodeError, tomllib.TOMLDecodeError) as error:
        raise InvalidArgument(
            "configuration source is not valid UTF-8 TOML",
            reason="configuration_source_parse",
            fields={"path": str(path)[:4096]},
            cause=error,
        ) from error

    return data, Source(path.stem, str(path), Digest.of(raw).text)


def _freeze(value: Any) -> Any:
    if isinstance(value, Mapping):
        return MappingProxyType({key: _freeze(item) for key, item in value.items()})
    if isinstance(value, list | tuple):
        return tuple(_freeze(item) for item in value)
    return value


def resolve(
    paths: Sequence[str | Path],
    *,
    overlays: Iterable[Mapping[str, Any]] = (),
    overrides: Sequence[str] = (),
) -> ResolvedConfig:
    if isinstance(paths, str | bytes | bytearray) or not isinstance(paths, Sequence):
        raise InvalidArgument(
            "configuration paths must be a sequence of filesystem paths",
            reason="configuration_source_records",
        )
    if len(paths) > MAXIMUM_SOURCES:
        raise ResourceExhausted(
            f"resolved configuration accepts at most {MAXIMUM_SOURCES} source files",
            reason="configuration_source_count",
        )
    try:
        overlay_iterator = iter(overlays)
    except TypeError as error:
        raise InvalidArgument(
            "configuration overlays must be iterable mappings",
            reason="configuration_overlay_records",
            cause=error,
        ) from error
    materialized_overlays = tuple(islice(overlay_iterator, MAXIMUM_OVERLAYS + 1))
    if len(materialized_overlays) > MAXIMUM_OVERLAYS:
        raise ResourceExhausted(
            f"resolved configuration accepts at most {MAXIMUM_OVERLAYS} overlays",
            reason="configuration_overlay_count",
        )
    if isinstance(overrides, str | bytes | bytearray) or not isinstance(overrides, Sequence):
        raise InvalidArgument(
            "configuration overrides must be a sequence of expressions",
            reason="configuration_override_records",
        )
    if len(overrides) > MAXIMUM_OVERRIDES:
        raise ResourceExhausted(
            f"resolved configuration accepts at most {MAXIMUM_OVERRIDES} overrides",
            reason="configuration_override_count",
        )
    value: dict[str, Any] = {}
    sources: list[Source] = []
    for item in paths:
        if not isinstance(item, str | Path):
            raise InvalidArgument(
                "configuration source entries must be filesystem paths",
                reason="configuration_source_records",
            )
        data, source = _load(Path(item))
        value = deep_merge(value, data)
        sources.append(source)
    for overlay in materialized_overlays:
        if not isinstance(overlay, Mapping):
            raise InvalidArgument(
                "configuration overlays must be mappings",
                reason="configuration_overlay_records",
            )
        value = deep_merge(value, overlay)
    applied: list[AppliedOverride] = []
    for expression in overrides:
        path, _ = apply_override(value, expression)
        applied.append(AppliedOverride(expression, path))
    digest = fingerprint(value)
    return ResolvedConfig(
        value=_freeze(value), digest=digest, sources=tuple(sources), overrides=tuple(applied)
    )

"""Composable TOML resolver used by training, inference, evaluation, and preprocessing."""

from __future__ import annotations

import tomllib
from collections.abc import Iterable, Mapping, Sequence
from dataclasses import dataclass
from pathlib import Path
from typing import Any

from .fingerprint import canonical_json, fingerprint
from .merge import deep_merge
from .overrides import apply_override


@dataclass(frozen=True)
class Source:
    name: str
    path: str
    digest: str


@dataclass(frozen=True)
class AppliedOverride:
    expression: str
    path: str


@dataclass(frozen=True)
class ResolvedConfig:
    value: Mapping[str, Any]
    digest: str
    sources: tuple[Source, ...]
    overrides: tuple[AppliedOverride, ...]
    schema_version: int = 1

    def canonical_bytes(self) -> bytes:
        return canonical_json(dict(self.value))


def _load(path: Path) -> tuple[dict[str, Any], Source]:
    raw = path.read_bytes()
    data = tomllib.loads(raw.decode("utf-8"))
    import hashlib

    return data, Source(path.stem, str(path), "sha256:" + hashlib.sha256(raw).hexdigest())


def resolve(
    paths: Sequence[str | Path],
    *,
    overlays: Iterable[Mapping[str, Any]] = (),
    overrides: Sequence[str] = (),
) -> ResolvedConfig:
    value: dict[str, Any] = {}
    sources: list[Source] = []
    for item in paths:
        data, source = _load(Path(item))
        value = deep_merge(value, data)
        sources.append(source)
    for overlay in overlays:
        value = deep_merge(value, overlay)
    applied = []
    for expression in overrides:
        path, _ = apply_override(value, expression)
        applied.append(AppliedOverride(expression, path))
    return ResolvedConfig(
        value=value, digest=fingerprint(value), sources=tuple(sources), overrides=tuple(applied)
    )

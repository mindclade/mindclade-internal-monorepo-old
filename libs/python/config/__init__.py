"""Mindclade deterministic resolved-configuration contracts."""

from .fingerprint import canonical_json, fingerprint
from .loader import AppliedOverride, ResolvedConfig, Source, resolve
from .merge import MergeError, deep_merge
from .overrides import OverrideError, apply_override

__all__ = [
    "AppliedOverride",
    "MergeError",
    "OverrideError",
    "ResolvedConfig",
    "Source",
    "apply_override",
    "canonical_json",
    "deep_merge",
    "fingerprint",
    "resolve",
]

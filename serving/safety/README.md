# Serving / Safety

**Status:** implemented policy orchestration; detector qualification remains deployment evidence.

This package composes injected, independently qualified screeners under an immutable content-
addressed policy. It bounds input and finding sizes, validates screener identity, fails closed when
a required screener is missing, fails, or overproduces, and emits only input digests and bounded
finding codes into its audit projection. Raw screened content is never part of an audit record.

No heuristic detector or policy content is embedded here. Detector models, thresholds, evaluation
sets, policy approval, and human review operations belong to their owning safety domains. An
`ALLOW` result means only that every required configured screener completed and produced no finding
at or above the review threshold; it is not a general safety guarantee.

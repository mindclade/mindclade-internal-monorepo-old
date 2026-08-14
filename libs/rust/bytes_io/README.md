# `mindclade_bytes_io`

Canonical checked byte sizes/ranges/alignment, RAII byte budgets, bounded copy
operations, and reusable buffers. Every substantial runtime allocation must be
covered by either a local byte reservation or the node-wide `runtime_core`
resource budget.

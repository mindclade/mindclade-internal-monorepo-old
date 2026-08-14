# Runtime host degraded

1. Stop new local admission and enter drain.
2. Preserve already-admitted work while tickets remain valid.
3. Inspect the bounded node diagnostics bundle and resource-budget tree.
4. Revoke or fence workers before replacement; never reuse stale fencing tokens.
5. Confirm Python worker termination before releasing GPU/model-slot reservations.
6. Escalate to node replacement when resource accounting is corrupted.

"""Model-worker composition seam.

Deployment-specific model loading and IPC adapters are intentionally injected;
this module never silently starts with fake providers.
"""

from __future__ import annotations


def main() -> int:
    raise SystemExit(
        "model worker requires deployment-owned IPC, batch planner, and PyTorch engine composition"
    )


if __name__ == "__main__":
    main()

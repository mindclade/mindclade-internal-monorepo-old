#!/usr/bin/env bash
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

set -euo pipefail
root="$(cd "$(dirname "$0")/../../../.." && pwd)"
cd "$root"
command -v nix >/dev/null
tools/dev/nixw flake lock
tools/dev/nixw develop .#ci --command tools/qualification/rust/update_lock.sh
tools/dev/nixw develop .#ci --command go mod tidy
python tools/dev/validate_repository.py
printf 'Lockfiles updated; review and commit flake.lock, Cargo.lock, go.mod, and go.sum.\n'

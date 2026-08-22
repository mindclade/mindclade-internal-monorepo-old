# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

set -euo pipefail

umask 0022
export HOME="${HOME:-/tmp/home}"
export TMPDIR="${TMPDIR:-/tmp}"
mkdir -p "$HOME" "$TMPDIR"

if [[ "$#" -eq 0 ]]; then
  exec /bin/bash --noprofile --norc
fi

exec "$@"

#!/usr/bin/env bash
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

set -euo pipefail

# Nix supplies every tool these jobs execute, while the hosted Ubuntu image reserves most of
# its disk for preinstalled mobile, .NET, Haskell, and CodeQL toolchains. A cold Nix realization
# can otherwise run out of space before the requested check starts. Fail closed on any runner
# whose lifecycle we do not control; these removals are valid only on GitHub's ephemeral hosts.
if [[ "${GITHUB_ACTIONS:-}" != "true" || "${RUNNER_ENVIRONMENT:-}" != "github-hosted" ]]; then
  echo "prepare_nix_runner.sh may run only on an ephemeral GitHub-hosted runner" >&2
  exit 2
fi
if [[ "$(uname -s)" != "Linux" ]]; then
  echo "prepare_nix_runner.sh supports Linux runners only" >&2
  exit 2
fi

df -h /
sudo rm -rf -- \
  /opt/ghc \
  /opt/hostedtoolcache/CodeQL \
  /usr/local/.ghcup \
  /usr/local/lib/android \
  /usr/share/dotnet
df -h /

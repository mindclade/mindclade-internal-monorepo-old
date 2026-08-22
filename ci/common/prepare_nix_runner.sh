#!/usr/bin/env bash
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

set -euo pipefail

# Nix supplies every tool these jobs execute, while the hosted Ubuntu image reserves most of
# its disk for preinstalled cloud, mobile, .NET, Haskell, language, and browser toolchains. A
# cold Nix realization plus a complete Bazel output tree can otherwise exhaust the runner after
# the requested check has started. Fail closed on any runner whose lifecycle we do not control;
# these removals are valid only on GitHub's ephemeral hosts. Actions execute with their bundled
# runtimes, and every repository toolchain is provided by Nix, so the hosted tool cache is not a
# trusted input to these jobs.
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
  /opt/az \
  /opt/ghc \
  /opt/hostedtoolcache \
  /opt/microsoft \
  /usr/local/.ghcup \
  /usr/local/aws-cli \
  /usr/local/aws-sam-cli \
  /usr/local/lib/android \
  /usr/local/share/chromium \
  /usr/local/share/powershell \
  /usr/local/share/vcpkg \
  /usr/share/dotnet \
  /usr/share/swift
df -h /

#!/usr/bin/env bash
# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

set -euo pipefail

readonly minimum_root_free_bytes=$((50 * 1024 * 1024 * 1024))

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
  /home/linuxbrew \
  /home/runner/.cargo \
  /home/runner/.nvm \
  /home/runner/.rustup \
  /opt/az \
  /opt/ghc \
  /opt/google \
  /opt/hostedtoolcache \
  /opt/microsoft \
  /opt/pipx \
  /usr/local/.ghcup \
  /usr/local/aws-cli \
  /usr/local/aws-sam-cli \
  /usr/local/kotlinc \
  /usr/local/lib/android \
  /usr/local/lib/node_modules \
  /usr/local/share/boost \
  /usr/local/share/chromium \
  /usr/local/share/powershell \
  /usr/local/share/vcpkg \
  /usr/share/dotnet \
  /usr/share/miniconda \
  /usr/share/swift
sudo find /usr/local -mindepth 1 -maxdepth 1 -type d -name 'julia*' -exec rm -rf -- {} +
df -h /

root_free_bytes="$(df --output=avail -B1 / | tail -n 1 | tr -d '[:space:]')"
readonly root_free_bytes
if [[ ! "${root_free_bytes}" =~ ^[0-9]+$ ]]; then
  echo "prepare_nix_runner.sh could not determine free space on /" >&2
  exit 2
fi
if ((root_free_bytes < minimum_root_free_bytes)); then
  printf \
    'prepare_nix_runner.sh requires at least %s bytes (50 GiB) free on / after cleanup; found %s bytes\n' \
    "${minimum_root_free_bytes}" \
    "${root_free_bytes}" >&2
  exit 1
fi

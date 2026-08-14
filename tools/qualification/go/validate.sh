#!/usr/bin/env bash
# Copyright 2026 Mindclade. All rights reserved.
# Confidential, proprietary, and trade-secret information.

set -euo pipefail

mode="${1:-offline}"
case "$mode" in
  offline|connected) ;;
  *)
    echo "usage: $0 [offline|connected]" >&2
    exit 64
    ;;
esac

if [[ -n "${BUILD_WORKSPACE_DIRECTORY:-}" ]]; then
  repo="$BUILD_WORKSPACE_DIRECTORY"
else
  script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
  repo="$(cd "$script_dir/../../.." && pwd)"
fi
cd "$repo"

fail() {
  echo "ERROR: $*" >&2
  exit 1
}

note() {
  printf '\n==> %s\n' "$*"
}

note "checking Go formatting"
mapfile -d '' go_files < <(
  find libs/go control services/control_plane examples/go \
    -type f -name '*.go' -print0 | sort -z
)
((${#go_files[@]} > 0)) || fail "no Go source files found"
unformatted="$(gofmt -l "${go_files[@]}")"
[[ -z "$unformatted" ]] || fail "unformatted Go files:\n$unformatted"

note "checking foundation shape"
[[ -z "$(find libs/go -type f \( -name go.mod -o -name go.sum \) -print -quit)" ]] \
  || fail "nested Go modules are prohibited under libs/go"
[[ -z "$(find libs/go -type f -size 0 -print -quit)" ]] \
  || fail "zero-byte files are prohibited under libs/go"
[[ -z "$(find . -type d -name __pycache__ -print -quit)" ]] \
  || fail "Python cache directories must not be committed"

note "checking Go dependency layers and paved roads"
PYTHONDONTWRITEBYTECODE=1 python3 tools/analysis/check_go_layers.py --repo "$repo"
PYTHONDONTWRITEBYTECODE=1 python3 tools/analysis/check_placeholder_packages.py --repo "$repo"

note "running race-enabled foundation and integration tests"
packages=(
  ./libs/go/config
  ./libs/go/messaging
  ./libs/go/messaging/memory
  ./libs/go/messaging/messagingtest
  ./libs/go/messaging/pubsub
  ./libs/go/pagination
  ./libs/go/resourceversion
  ./libs/go/signing
  ./libs/go/httpx/outbound
  ./libs/go/storage/sql/migrate
  ./libs/go/servicekit
  ./libs/go/servicekit/production
  ./libs/go/coordination/...
  ./services/control_plane/internal/bootstrap
  ./services/control_plane/internal/foundation
  ./examples/go/event_dispatcher
  ./examples/go/ingestion_coordinator
)
GOWORK=off go test -race -count=1 "${packages[@]}"

note "running reference vertical slices"
GOWORK=off go run ./examples/go/event_dispatcher
GOWORK=off go run ./examples/go/ingestion_coordinator

note "validating representative production role manifests"
for role in api scheduler ingestion_controller event_dispatcher registry; do
  GOWORK=off go run "./services/control_plane/cmd/$role" --describe-profile >/dev/null
done

if [[ "$mode" == "connected" ]]; then
  note "downloading and verifying pinned Go modules"
  GOWORK=off go mod download all
  [[ -s go.sum ]] || fail "go.sum was not populated"
  GOWORK=off go mod verify

  note "checking that module metadata is tidy without mutating the checkout"
  tidy_diff="$(GOWORK=off go mod tidy -diff)"
  [[ -z "$tidy_diff" ]] || fail "go.mod/go.sum are not tidy; run go mod tidy in the pinned connected environment:\n$tidy_diff"

  note "running connected Go qualification"
  GOWORK=off go vet ./libs/go/... ./control/... ./services/control_plane/... ./examples/go/...
  GOWORK=off go test -race -count=1 ./libs/go/... ./control/... ./services/control_plane/... ./examples/go/...

  if command -v bazel >/dev/null 2>&1; then
    note "running Bazel Go qualification"
    bazel test \
      //libs/go/... \
      //services/control_plane/internal/... \
      //examples/go/...
  else
    fail "connected mode requires Bazel on PATH"
  fi
fi

note "Go foundation qualification passed ($mode mode)"

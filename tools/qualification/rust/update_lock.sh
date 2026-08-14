#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "$0")/../../.." && pwd)"
cd "$root"
expected="1.97.1"
actual="$(rustc --version | awk '{print $2}')"
if [[ "$actual" != "$expected" ]]; then
  echo "expected rustc $expected, got $actual" >&2
  exit 1
fi
cargo generate-lockfile
cargo metadata --locked --format-version=1 >/dev/null
cargo verify-project >/dev/null
cargo fmt --all -- --check
python tools/qualification/rust/supply_chain.py --connected
printf 'updated Cargo.lock with rustc %s\n' "$actual"

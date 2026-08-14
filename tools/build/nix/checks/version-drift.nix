{ pkgs, root, versions, ... }:
pkgs.runCommand "mindclade-version-drift" { nativeBuildInputs = [ pkgs.python3 ]; } ''
  python - <<'PY2'
from pathlib import Path
import re
root=Path("${root}")
cargo=(root/'Cargo.toml').read_text()
m=re.search(r'rust-version\\s*=\\s*"([^"]+)"', cargo)
if not m or m.group(1) != "${versions.rust}":
    raise SystemExit('Cargo/Nix Rust version drift')
PY2
  touch "$out"

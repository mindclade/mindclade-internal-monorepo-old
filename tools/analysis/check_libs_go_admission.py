#!/usr/bin/env python3
from __future__ import annotations
import argparse,tomllib
from pathlib import Path
def check(root:Path)->list[str]:
 cfg=tomllib.loads((root/'libs/go/ADMISSION.toml').read_text());allowed=set(cfg['allowed_top_level']);forbidden=set(cfg['forbidden_names']);errors=[]
 actual={p.name for p in (root/'libs/go').iterdir() if p.is_dir() and any(p.rglob('*.go'))}
 for x in sorted(actual-allowed): errors.append(f'libs/go/{x}: new top-level library is not admitted')
 for x in sorted(actual & forbidden): errors.append(f'libs/go/{x}: forbidden dumping-ground/domain name')
 return errors
def main():
 ap=argparse.ArgumentParser();ap.add_argument('--repo',type=Path,default=Path(__file__).resolve().parents[2]);a=ap.parse_args();e=check(a.repo.resolve());[print(x) for x in e];print('libs/go admission check passed' if not e else 'libs/go admission check failed');return 1 if e else 0
if __name__=='__main__':raise SystemExit(main())

#!/usr/bin/env python3
from __future__ import annotations
import os,subprocess,sys
from pathlib import Path
ROOT=Path(__file__).resolve().parents[3]
results=os.environ.get('MINDCLADE_RUST_PERFORMANCE_RESULTS')
if not results:
    print('MINDCLADE_RUST_PERFORMANCE_RESULTS must point to hardware/provider benchmark JSON',file=sys.stderr); raise SystemExit(1)
cmd=[sys.executable,str(ROOT/'tools/qualification/rust/performance.py'),'--measure','--results',results,'--require-complete']
raise SystemExit(subprocess.call(cmd,cwd=ROOT))

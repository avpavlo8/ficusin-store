#!/usr/bin/env python3
from pathlib import Path

root = Path(__file__).resolve().parents[1]
path = root / "scripts/catalog_ui_followup.py"
text = path.read_text(encoding="utf-8")
old = 'css = ROOT / "frontend/src/index.css"'
new = 'css = ROOT / "frontend/src/styles/admin.css"'
if old in text:
    path.write_text(text.replace(old, new, 1), encoding="utf-8")
    print("patched catalogue UI stylesheet target")
elif new in text:
    print("catalogue UI stylesheet target already patched")
else:
    raise RuntimeError("catalogue UI stylesheet target not found")

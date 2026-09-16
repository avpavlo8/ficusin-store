from pathlib import Path

path = Path(__file__).with_name("oneoff_procurement_reconciliation_workflow.py")
text = path.read_text()
old = "branch = r'''\\tif err != nil {"
new = "branch = '''\tif err != nil {"
if old not in text:
    raise SystemExit("branch string anchor not found")
path.write_text(text.replace(old, new, 1))

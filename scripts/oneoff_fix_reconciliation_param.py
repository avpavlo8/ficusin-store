from pathlib import Path

path = Path(__file__).resolve().parents[1] / "backend/internal/procurement/postgres.go"
text = path.read_text()
old = "reconciliation_status=CASE WHEN ordered_qty<>$7 OR expected_unit_price IS NULL OR $8 IS NULL OR\n\t\t\t\t\t\tABS(expected_unit_price-$8)>.005"
new = "reconciliation_status=CASE WHEN ordered_qty<>$7 OR expected_unit_price IS NULL OR\n\t\t\t\t\t\tABS(expected_unit_price-($8::NUMERIC))>.005"
if old not in text:
    raise SystemExit("reconciliation price CASE anchor not found")
path.write_text(text.replace(old, new, 1))

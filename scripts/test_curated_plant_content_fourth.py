#!/usr/bin/env python3
"""Guard the fourth launch-content batch against identity and overwrite drift."""
from __future__ import annotations

import json
import unittest
from pathlib import Path

from generate_curated_plant_content_fourth import OUT, PROFILES, build

ROOT = Path(__file__).resolve().parents[1]
IDENTITY_SNAPSHOT = ROOT / 'docs/catalog-curated-content-fourth-identity-snapshot-2026-10-12.json'
DIMENSIONS = ROOT / 'timeweb/migrations/20261012_zzzzzzz_exact_pdf_variant_dimensions.sql'


class FourthContentTests(unittest.TestCase):
    def test_exact_names_from_independent_pdf_site_audit(self):
        rows = json.loads(IDENTITY_SNAPSHOT.read_text())['cards']
        by_code = {row['code']: row['name'] for row in rows}
        self.assertEqual(len(rows), len(by_code), 'Duplicate independent identities')
        self.assertEqual(set(PROFILES), set(by_code))
        for code, profile in PROFILES.items():
            self.assertEqual(profile[0], by_code[code], code)

    def test_generated_sql_is_current_and_blank_only(self):
        self.assertEqual(OUT.read_text(), build())
        sql = OUT.read_text()
        self.assertEqual(sql.count('UPDATE products AS p SET'), 1)
        for field in ('latin_name', 'short_description', 'description', 'care_instructions', 'light_level', 'watering'):
            self.assertIn(f"BTRIM(COALESCE(p.{field},'')) = '' THEN", sql)
        self.assertIn("p.catalog_section = 'plants' AND p.name = c.expected_name", sql)
        for forbidden in ('base_price_minor', 'stock_quantity', 'image_url', 'product_external_ids', 'archived_at'):
            self.assertNotIn(forbidden, sql)

    def test_dimensions_require_exact_saby_identity_and_blank_value(self):
        sql = DIMENSIONS.read_text()
        for x in ('X1150532', 'X1933443', 'X922724685'):
            self.assertIn(x, sql)
        self.assertIn('product.name=pdf.expected_name', sql)
        self.assertIn('variant_numeric_attribute(variant.id,pdf.attribute_code) IS NULL', sql)
        self.assertIn("external.provider='saby'", sql)
        self.assertIn('count(*) FROM product_variants AS sibling', sql)
        for forbidden in ('base_price_minor', 'stock_quantity', 'image_url'):
            self.assertNotIn(forbidden, sql)


if __name__ == '__main__':
    unittest.main()

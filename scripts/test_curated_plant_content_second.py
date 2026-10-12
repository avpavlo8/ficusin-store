#!/usr/bin/env python3
import json
import re
import unittest
from pathlib import Path
import generate_curated_plant_content_second as batch

ROOT = Path(__file__).resolve().parents[1]
FIRST = ROOT / 'docs/catalog-curated-content-snapshot-2026-10-12.json'

class CuratedSecondTest(unittest.TestCase):
    def test_disjoint_exact_snapshot(self):
        names = json.loads(batch.SNAPSHOT.read_text())
        first = set(json.loads(FIRST.read_text()))
        ids = [str(code) for codes in batch.ASSIGNMENTS.values() for code in codes]
        self.assertEqual(set(ids), set(names))
        self.assertEqual(len(ids), len(set(ids)))
        self.assertEqual(set(ids) & first, {'27'})
        sql = batch.MIGRATION.read_text()
        self.assertEqual(len(re.findall(r'^  \(\d+,', sql, re.M)), len(ids) + len(list(batch.passport_rows())))
        for code, name in names.items():
            self.assertIn(f"({code},'{name.replace(chr(39), chr(39)*2)}'", sql)

    def test_primary_sources_and_specific_text(self):
        for key, profile in batch.PROFILES.items():
            source, latin, short, description, care, light, watering = profile
            self.assertRegex(source, r'^https://(www\.rhs\.org\.uk|plants\.ces\.ncsu\.edu)/', key)
            self.assertGreater(len(short), 45, key)
            self.assertGreater(len(description), 100, key)
            self.assertGreater(len(care), 100, key)
            self.assertIn(light, ('sunny', 'diffused', 'low_light'))
            self.assertIn(watering, ('frequent', 'moderate', 'rare'))

    def test_guarded_noncommercial_change(self):
        sql = batch.MIGRATION.read_text()
        self.assertIn("p.product_code = c.product_code AND p.catalog_section = 'plants' AND p.name = c.expected_name", sql)
        for forbidden in ('base_price_minor', 'stock_quantity =', 'status =', 'height_cm', 'pot_diameter_cm', 'image_url', 'saby_id ='):
            self.assertNotIn(forbidden, sql)
        self.assertIn("BTRIM(p.short_description) = ''", sql)
        self.assertIn("COALESCE(BTRIM(p.light_level),'') = ''", sql)
        self.assertIn("name = 'Алоказия Ред Секрет' || U&'\\00A0'", sql)
        self.assertIn("p.plant_passport->>'lighting'", sql)
        self.assertIn("p.plant_passport->>'watering'", sql)
        self.assertIn("COALESCE(p.plant_passport, '{}'::jsonb)", sql)
        self.assertEqual(len(list(batch.passport_rows())), len(set(code for code, *_ in batch.passport_rows())))
        preview = batch.PREVIEW.read_text()
        self.assertEqual(len(re.findall(r'^  \(\d+,', preview, re.M)), len(json.loads(batch.SNAPSHOT.read_text())))
        self.assertIn("THEN 'will_update' ELSE 'already_complete'", preview)

    def test_regeneration_stable(self):
        before = batch.MIGRATION.read_bytes(), batch.PREVIEW.read_bytes()
        batch.main()
        self.assertEqual(before, (batch.MIGRATION.read_bytes(), batch.PREVIEW.read_bytes()))

if __name__ == '__main__':
    unittest.main()

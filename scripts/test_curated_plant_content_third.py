#!/usr/bin/env python3
"""Guard the third plant-content release against broad or destructive edits."""
import json
import re
import unittest
from pathlib import Path

import generate_curated_plant_content_third as batch


class CuratedThirdTest(unittest.TestCase):
    def test_exact_snapshot_and_source_quality(self):
        names = {int(k): v for k, v in json.loads(batch.SNAPSHOT.read_text()).items()}
        ids = [code for group in batch.ASSIGNMENTS.values() for code in group]
        self.assertEqual(len(ids), len(set(ids)))
        self.assertEqual(set(ids), set(names))
        provenance = json.loads((Path(__file__).resolve().parents[1] / 'docs/catalog-curated-content-third-pdf-codes-2026-10-12.json').read_text())
        self.assertEqual(set(map(str, ids)), set(provenance))
        self.assertTrue(all(re.fullmatch(r'X\d+', code) for code in provenance.values()))
        self.assertGreaterEqual(len(ids), 40)
        for key, profile in batch.PROFILES.items():
            source, latin, short, description, care, light, watering = profile
            self.assertRegex(source, r'^https://(www\.rhs\.org\.uk|plants\.ces\.ncsu\.edu)/', key)
            self.assertGreater(len(short), 40, key)
            self.assertGreater(len(description), 95, key)
            self.assertGreater(len(care), 100, key)
            self.assertIn(light, ('sunny', 'diffused', 'low_light'))
            self.assertIn(watering, ('frequent', 'moderate', 'rare'))
            lighting, water = batch.passport_advice(profile)
            self.assertNotEqual(lighting, water)
            self.assertNotEqual(water, 'Поддерживайте влажность воздуха.')

    def test_sql_is_blank_only_and_exact_for_taxonomy(self):
        sql = batch.MIGRATION.read_text()
        self.assertEqual(len(re.findall(r'^  \(\d+,', sql, re.M)), sum(map(len, batch.ASSIGNMENTS.values())))
        self.assertIn("p.product_code = c.product_code AND p.catalog_section = 'plants' AND p.name = c.expected_name", sql)
        self.assertIn("BTRIM(COALESCE(p.short_description,'')) = ''", sql)
        self.assertIn("BTRIM(COALESCE(p.plant_passport->>'lighting','')) = ''", sql)
        self.assertIn("BTRIM(COALESCE(p.plant_passport->>'watering','')) = ''", sql)
        for forbidden in ('base_price_minor', 'stock_quantity =', 'status =', 'height_cm', 'pot_diameter_cm', 'image_url', 'saby_id ='):
            self.assertNotIn(forbidden, sql)
        for code, name, old, new, source in batch.BOTANICAL_CORRECTIONS:
            self.assertIn(f'product_code={code}', sql)
            self.assertIn(f'-- Botanical correction: {source}', sql)
            self.assertIn(f'latin_name={batch.q(old)}', sql)
            self.assertIn(f'name={batch.q(name)}', sql)

    def test_regeneration_is_stable(self):
        before = batch.MIGRATION.read_bytes()
        batch.main()
        self.assertEqual(before, batch.MIGRATION.read_bytes())

    def test_pdf_diameters_are_single_variant_and_external_code_guarded(self):
        root = Path(__file__).resolve().parents[1]
        rows = json.loads((root / 'docs/catalog-pot-diameters-pdf-2026-10-12.json').read_text())
        sql = (root / 'timeweb/migrations/20261012_zzzz_pdf_plant_pot_diameters.sql').read_text()
        self.assertEqual(len(rows), 15)
        self.assertEqual(len({row['product_code'] for row in rows}), len(rows))
        for row in rows:
            self.assertRegex(row['expected_name'], rf'(?i)(?<![A-ZА-Я0-9])[DД]\s*{row["pot_diameter_cm"]}(?!\d)')
            self.assertEqual(row['expected_name'].replace('  ', ' '), row['pdf_name'])
            self.assertIn(f"({row['product_code']},'{row['expected_name'].replace(chr(39), chr(39)*2)}','{row['saby_code']}',{row['pot_diameter_cm']})", sql)
        self.assertIn("external.provider='saby' AND external.id_type='code'", sql)
        self.assertIn('sibling.is_active<>0 AND sibling.archived_at IS NULL)=1', sql)
        self.assertIn('variant.pot_diameter_cm IS NULL', sql)
        for forbidden in ('height_cm=', 'base_price_minor', 'stock_quantity=', 'status=', 'image_url='):
            self.assertNotIn(forbidden, sql)


if __name__ == '__main__':
    unittest.main()

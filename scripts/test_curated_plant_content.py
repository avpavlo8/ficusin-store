#!/usr/bin/env python3
import importlib.util
import json
import re
import unittest
from pathlib import Path
ROOT=Path(__file__).resolve().parents[1]
spec=importlib.util.spec_from_file_location('curated',ROOT/'scripts/generate_curated_plant_content.py')
module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)
class CuratedContentTest(unittest.TestCase):
 def test_snapshot_and_migration_exact_match(self):
  names=json.loads(module.LIVE.read_text())
  ids=[id for values in module.ASSIGNMENTS.values() for id in values]
  self.assertEqual(len(ids),len(names))
  self.assertEqual(len(set(ids)),len(names))
  self.assertEqual(set(map(str,ids)),set(names))
  sql=module.MIGRATION.read_text()
  self.assertEqual(len(re.findall(r'^  \(\d+,',sql,re.M)),len(names))
  for id,name in names.items():
   self.assertIn(f"({id},'{name.replace(chr(39),chr(39)*2)}'",sql)
 def test_every_profile_has_primary_source_and_specific_copy(self):
  for key,profile in module.PROFILES.items():
   source,latin,short,description,care,light,water=profile
   self.assertRegex(source,r'^https://(www\.rhs\.org\.uk|plants\.ces\.ncsu\.edu)/')
   self.assertGreater(len(short),45,key)
   self.assertGreater(len(description),100,key)
   self.assertGreater(len(care),100,key)
   self.assertIn(light,('sunny','diffused','low_light'))
   self.assertIn(water,('frequent','moderate','rare'))
 def test_guard_and_no_commercial_columns(self):
  sql=module.MIGRATION.read_text()
  self.assertIn("p.product_code = c.product_code AND p.catalog_section = 'plants' AND p.name = c.expected_name",sql)
  self.assertIn("category.slug = 'paml'",sql)
  self.assertIn('NOT EXISTS (SELECT 1 FROM products AS product WHERE product.category_id = category.id)',sql)
  for field in ('latin_name','short_description','description','care_instructions','light_level','watering'):
   self.assertRegex(sql,rf'BTRIM\(p\.{field}\)')
  for forbidden in ('base_price_minor','stock_quantity =','status =','height_cm','pot_diameter_cm','image_url','saby_id =','variant_id ='):
   self.assertNotIn(forbidden,sql)
 def test_regeneration_is_stable(self):
  before=module.MIGRATION.read_bytes()
  module.main()
  self.assertEqual(module.MIGRATION.read_bytes(),before)
if __name__=='__main__':unittest.main()

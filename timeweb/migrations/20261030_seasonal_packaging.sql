-- Existing package dimensions remain the summer set; do not invent winter sizes.
INSERT INTO attribute_definitions(code,name,data_type,unit,audience,value_scope,is_global,is_active,description) VALUES
 ('package_winter_length_cm','Длина зимней упаковки','number','см','technical','variant',FALSE,TRUE,'Наружная длина зимней коробки'),
 ('package_winter_width_cm','Ширина зимней упаковки','number','см','technical','variant',FALSE,TRUE,'Наружная ширина зимней коробки'),
 ('package_winter_height_cm','Высота зимней упаковки','number','см','technical','variant',FALSE,TRUE,'Наружная высота зимней коробки');

-- Follow the same category assignments as the corresponding summer dimensions.
INSERT INTO category_attributes(category_id,attribute_id,is_required,is_filterable,show_on_pdp,is_badge,sort_order,show_in_summary,summary_position,show_in_characteristics,is_excluded)
SELECT assignment.category_id,winter.id,FALSE,FALSE,FALSE,FALSE,assignment.sort_order+10,FALSE,NULL,FALSE,assignment.is_excluded
FROM category_attributes assignment
JOIN attribute_definitions summer ON summer.id=assignment.attribute_id
JOIN attribute_definitions winter ON winter.code=REPLACE(summer.code,'package_','package_winter_')
WHERE summer.code IN ('package_length_cm','package_width_cm','package_height_cm');

INSERT INTO settings(key,value) VALUES ('cdek.packaging_season','summer') ON CONFLICT(key) DO NOTHING;
ALTER TABLE settings ADD CONSTRAINT settings_packaging_season_valid
 CHECK (key <> 'cdek.packaging_season' OR value IN ('summer','winter'));

-- One resolver for quotes, checkout and new packing suggestions. Missing winter
-- dimensions stay NULL: no mixing seasons or silently pricing a smaller box.
CREATE FUNCTION shipping_package_dimension(target_variant BIGINT, attribute_code TEXT)
RETURNS NUMERIC LANGUAGE sql STABLE AS $$
 SELECT variant_numeric_attribute(target_variant,
   CASE WHEN (SELECT value FROM settings WHERE key='cdek.packaging_season')='winter'
     THEN REPLACE(attribute_code,'package_','package_winter_')
     ELSE attribute_code END)
$$;

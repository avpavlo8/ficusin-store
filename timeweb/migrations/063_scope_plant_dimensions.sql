-- 062 removed cross-category assignments, but old production metadata may
-- still mark plant dimensions as global. Global definitions are appended to
-- every effective schema, so explicitly scope them back to the plant tree.
UPDATE attribute_definitions
SET is_global=FALSE
WHERE code IN ('height_cm','pot_diameter_cm');

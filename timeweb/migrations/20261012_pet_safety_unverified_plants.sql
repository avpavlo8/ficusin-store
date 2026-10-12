-- Species-level non-toxicity does not establish the safety of a dyed flower,
-- and there is no primary veterinary non-toxicity listing for Metasequoia.
-- Keep both plants for sale, but remove them from the pet-safe collection
-- until their exact treatment/species can be verified. The legacy-column
-- trigger copies the new value into canonical product_attribute_values.
UPDATE products
SET pet_safety='unknown', updated_at=CURRENT_TIMESTAMP
WHERE (product_code=543 AND name ILIKE '%Метасеквойя%')
   OR (product_code=829 AND name ILIKE '%Фаленопсис%крашен%');

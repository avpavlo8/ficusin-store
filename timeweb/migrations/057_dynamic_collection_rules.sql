-- Dynamic collection predicates are evaluated from the canonical PIM values.
-- Manual collections continue to use collection_products. Dynamic collections
-- never copy their membership into that table, so attribute edits are visible
-- immediately without a rebuild job.

CREATE OR REPLACE FUNCTION collection_rule_value_matches(actual JSONB, rule JSONB)
RETURNS BOOLEAN
LANGUAGE plpgsql
IMMUTABLE
AS $$
DECLARE
  op TEXT := COALESCE(rule->>'operator', 'eq');
  expected JSONB := rule->'value';
  actual_text TEXT;
  expected_text TEXT;
  actual_number NUMERIC;
  expected_number NUMERIC;
BEGIN
  IF op = 'exists' THEN
    RETURN actual IS NOT NULL AND actual <> 'null'::JSONB;
  END IF;
  IF actual IS NULL OR actual = 'null'::JSONB OR expected IS NULL THEN
    RETURN FALSE;
  END IF;

  actual_text := CASE WHEN jsonb_typeof(actual) = 'string' THEN actual #>> '{}' ELSE actual::TEXT END;
  expected_text := CASE WHEN jsonb_typeof(expected) = 'string' THEN expected #>> '{}' ELSE expected::TEXT END;

  IF op = 'eq' THEN
    RETURN actual = expected OR actual_text = expected_text;
  ELSIF op = 'neq' THEN
    RETURN NOT (actual = expected OR actual_text = expected_text);
  ELSIF op = 'in' THEN
    IF jsonb_typeof(expected) <> 'array' THEN
      RETURN FALSE;
    END IF;
    IF jsonb_typeof(actual) = 'array' THEN
      RETURN EXISTS (
        SELECT 1
        FROM jsonb_array_elements_text(actual) current_value
        JOIN jsonb_array_elements_text(expected) wanted_value ON current_value = wanted_value
      );
    END IF;
    RETURN EXISTS (SELECT 1 FROM jsonb_array_elements_text(expected) wanted WHERE wanted = actual_text);
  ELSIF op = 'contains' THEN
    IF jsonb_typeof(actual) <> 'array' THEN
      RETURN FALSE;
    END IF;
    IF jsonb_typeof(expected) = 'array' THEN
      RETURN actual @> expected;
    END IF;
    RETURN EXISTS (SELECT 1 FROM jsonb_array_elements_text(actual) current_value WHERE current_value = expected_text);
  ELSIF op IN ('gte', 'lte') THEN
    BEGIN
      actual_number := actual_text::NUMERIC;
      expected_number := expected_text::NUMERIC;
    EXCEPTION WHEN invalid_text_representation THEN
      RETURN FALSE;
    END;
    IF op = 'gte' THEN RETURN actual_number >= expected_number; END IF;
    RETURN actual_number <= expected_number;
  END IF;

  RETURN FALSE;
END;
$$;

CREATE OR REPLACE FUNCTION collection_rules_match_product(target_product BIGINT, rule_set JSONB)
RETURNS BOOLEAN
LANGUAGE plpgsql
STABLE
AS $$
DECLARE
  rule JSONB;
  definition_id BIGINT;
  definition_scope TEXT;
  matched BOOLEAN;
BEGIN
  IF rule_set IS NULL OR jsonb_typeof(rule_set) <> 'array' OR jsonb_array_length(rule_set) = 0 THEN
    RETURN FALSE;
  END IF;

  FOR rule IN SELECT value FROM jsonb_array_elements(rule_set)
  LOOP
    SELECT id, value_scope INTO definition_id, definition_scope
    FROM attribute_definitions
    WHERE code = BTRIM(rule->>'attribute') AND is_active
    LIMIT 1;

    IF definition_id IS NULL THEN
      RETURN FALSE;
    END IF;

    IF definition_scope = 'product' THEN
      SELECT COALESCE(BOOL_OR(collection_rule_value_matches(value, rule)), FALSE)
      INTO matched
      FROM product_attribute_values
      WHERE product_id = target_product AND attribute_id = definition_id;
    ELSE
      SELECT COALESCE(BOOL_OR(collection_rule_value_matches(attribute_value.value, rule)), FALSE)
      INTO matched
      FROM variant_attribute_values attribute_value
      JOIN product_variants variant ON variant.id = attribute_value.variant_id
      WHERE variant.product_id = target_product
        AND variant.is_active = 1
        AND variant.archived_at IS NULL
        AND attribute_value.attribute_id = definition_id;
    END IF;

    IF NOT matched THEN
      RETURN FALSE;
    END IF;
  END LOOP;

  RETURN TRUE;
END;
$$;

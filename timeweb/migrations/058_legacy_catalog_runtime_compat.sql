-- Emergency compatibility bridge for a rolling deployment.
--
-- Migration 056 changed order_items.product_id from the legacy TEXT product
-- reference to the real BIGINT products.id. A failed Timeweb rollout can leave
-- the previous application container serving traffic against the migrated
-- shared database. That previous catalogue query joins popularity.product_id
-- (now BIGINT) to products.slug (TEXT), which PostgreSQL rejects and the
-- storefront receives HTTP 503 from /api/v1/catalog.
--
-- Keep the previous container readable until the new runtime is proven healthy.
-- The current application does not depend on this operator. Remove it in a
-- later migration after production has fully switched to catalogue v2.

CREATE OR REPLACE FUNCTION public.ficusin_legacy_bigint_text_eq(left_value BIGINT, right_value TEXT)
RETURNS BOOLEAN
LANGUAGE SQL
IMMUTABLE
STRICT
PARALLEL SAFE
AS $$
  SELECT left_value::TEXT = right_value
$$;

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1
    FROM pg_operator
    WHERE oprname = '='
      AND oprleft = 'bigint'::regtype
      AND oprright = 'text'::regtype
  ) THEN
    EXECUTE $operator$
      CREATE OPERATOR public.= (
        LEFTARG = BIGINT,
        RIGHTARG = TEXT,
        PROCEDURE = public.ficusin_legacy_bigint_text_eq
      )
    $operator$;
  END IF;
END;
$$;

-- Compile and execute the exact cross-type comparison used by the previous
-- catalogue runtime. If operator resolution is wrong, fail this migration in
-- CI rather than discovering it on the live storefront.
DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1
    FROM (VALUES (1::BIGINT)) AS legacy_order(product_id)
    JOIN (VALUES ('1'::TEXT)) AS legacy_product(slug)
      ON legacy_order.product_id = legacy_product.slug
  ) THEN
    RAISE EXCEPTION 'legacy BIGINT = TEXT catalogue compatibility check failed';
  END IF;
END;
$$;

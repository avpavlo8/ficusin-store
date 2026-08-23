-- Remove the emergency bridge from migration 058.
--
-- 058 created a global `=` operator for (BIGINT, TEXT) so the previous
-- application container could keep serving the storefront while the database
-- had already moved to catalogue v2. Production now runs catalogue v2, so the
-- operator has no reader left, and a custom equality operator on base types is
-- exactly the kind of thing that makes a future query resolve to something
-- nobody intended.
--
-- 058 itself said this should be removed once production had switched. It has.

DROP OPERATOR IF EXISTS public.= (BIGINT, TEXT);
DROP FUNCTION IF EXISTS public.ficusin_legacy_bigint_text_eq(BIGINT, TEXT);

DO $$
BEGIN
  IF EXISTS (
    SELECT 1
    FROM pg_operator
    WHERE oprname = '='
      AND oprleft = 'bigint'::regtype
      AND oprright = 'text'::regtype
      AND oprnamespace = 'public'::regnamespace
  ) THEN
    RAISE EXCEPTION 'legacy BIGINT = TEXT operator is still installed';
  END IF;
END;
$$;

-- An operator may correct only the configured native regulation of an already
-- acknowledged, empty and unposted origin. Original requests/revisions stay
-- immutable. Every approval retains both request/progress versions and proof.
CREATE TABLE order_sale_regulation_amendments (
 id BIGSERIAL PRIMARY KEY,
 publication_id BIGINT NOT NULL UNIQUE REFERENCES order_sale_publications(id) ON DELETE RESTRICT,
 intent_id BIGINT NOT NULL REFERENCES order_sale_intents(id) ON DELETE RESTRICT,
 actor_customer_id BIGINT NOT NULL REFERENCES customers(id) ON DELETE RESTRICT,
 approved_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
 before_request JSONB NOT NULL,
 before_hash TEXT NOT NULL CHECK(before_hash ~ '^[a-f0-9]{64}$'),
 effective_request JSONB NOT NULL,
 effective_hash TEXT NOT NULL CHECK(effective_hash ~ '^[a-f0-9]{64}$'),
 before_progress JSONB NOT NULL,
 effective_progress JSONB NOT NULL,
 proof JSONB NOT NULL,
 document_id BIGINT NOT NULL CHECK(document_id>0),
 document_uuid UUID NOT NULL UNIQUE,
 CHECK(before_hash<>effective_hash),
 CHECK(before_request-'regulationId'=effective_request-'regulationId'),
 CHECK((before_request->>'regulationId')::BIGINT>0 AND (effective_request->>'regulationId')::BIGINT>0),
 CHECK(before_request->>'regulationId'<>effective_request->>'regulationId'),
 CHECK(before_progress-'native'=effective_progress-'native'),
 CHECK(jsonb_typeof(before_progress->'native')='object' AND jsonb_typeof(effective_progress->'native')='object'),
 CHECK((before_progress->'native')-'requestHash'=(effective_progress->'native')-'requestHash'),
 CHECK(before_progress#>>'{native,requestHash}'=before_hash),
 CHECK(effective_progress#>>'{native,requestHash}'=effective_hash),
 CHECK(proof->>'beforeHash'=before_hash AND proof->>'effectiveHash'=effective_hash),
 CHECK((proof->>'documentId')::BIGINT=document_id AND (proof->>'documentUUID')::UUID=document_uuid)
);
CREATE UNIQUE INDEX order_sale_regulation_amendments_native_id_unique ON order_sale_regulation_amendments(document_id);
CREATE FUNCTION order_sale_regulation_amendment_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE p order_sale_publications; c order_sale_dispatch_control;
BEGIN
 IF TG_OP<>'INSERT' THEN RAISE EXCEPTION 'order sale regulation approval is immutable'; END IF;
 SELECT * INTO p FROM order_sale_publications WHERE id=NEW.publication_id;
 SELECT * INTO c FROM order_sale_dispatch_control WHERE singleton;
 IF p.id IS NULL OR NEW.intent_id<>p.intent_id OR p.execution_mode<>'live'
  OR p.request<>NEW.before_request OR p.request_hash<>NEW.before_hash OR p.progress<>NEW.before_progress
  OR p.status<>'needs_attention' OR p.desired_state<>'posted' OR p.cancellation_blocked
  OR NOT p.create_attempted OR p.post_attempted OR p.stock_verified OR p.cancellation_verified
  OR p.document_id<>'' OR p.document_uuid<>'' OR p.lease_token<>'' OR p.lease_until IS NOT NULL
  OR p.progress->>'stage'<>'discover' OR COALESCE((p.progress->>'unknown')::BOOLEAN,FALSE)
  OR NOT c.enabled OR c.execution_mode<>'live' OR (NEW.effective_request->>'regulationId')::BIGINT<>c.regulation_id THEN
  RAISE EXCEPTION 'order sale regulation approval source changed';
 END IF;
 IF NOT EXISTS(SELECT 1 FROM admin_users WHERE customer_id=NEW.actor_customer_id AND role='owner' AND is_active) THEN
  RAISE EXCEPTION 'order sale regulation approval requires active owner';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER order_sale_regulation_amendments_guard BEFORE INSERT OR UPDATE OR DELETE ON order_sale_regulation_amendments
 FOR EACH ROW EXECUTE FUNCTION order_sale_regulation_amendment_guard();

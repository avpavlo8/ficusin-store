-- Keep prior links when an operator changes a channel's seller article.
CREATE TABLE IF NOT EXISTS catalog_editor_link_history (
 id BIGSERIAL PRIMARY KEY,
 variant_id BIGINT NOT NULL REFERENCES product_variants(id) ON DELETE CASCADE,
 channel TEXT NOT NULL,
 execution_mode TEXT NOT NULL,
 old_remote_id TEXT NOT NULL,
 old_offer_id TEXT NOT NULL,
 new_remote_id TEXT NOT NULL,
 new_offer_id TEXT NOT NULL,
 changed_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE OR REPLACE FUNCTION catalog_editor_record_relink() RETURNS trigger AS $$
BEGIN
 IF (OLD.remote_id,OLD.offer_id) IS DISTINCT FROM (NEW.remote_id,NEW.offer_id) THEN
  INSERT INTO catalog_editor_link_history(variant_id,channel,execution_mode,old_remote_id,old_offer_id,new_remote_id,new_offer_id)
  VALUES(NEW.variant_id,NEW.channel,NEW.execution_mode,OLD.remote_id,OLD.offer_id,NEW.remote_id,NEW.offer_id);
 END IF;
 RETURN NEW;
END $$ LANGUAGE plpgsql;
CREATE TRIGGER catalog_editor_record_relink AFTER UPDATE ON catalog_editor_remote_links
 FOR EACH ROW EXECUTE FUNCTION catalog_editor_record_relink();

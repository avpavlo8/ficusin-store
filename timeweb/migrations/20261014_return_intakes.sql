-- Editable intake drafts are separate from the financial/physical return ledger.
CREATE TABLE return_intakes (
  id UUID PRIMARY KEY,
  created_by BIGINT NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
  revision BIGINT NOT NULL DEFAULT 1,
  data JSONB NOT NULL,
  status TEXT NOT NULL DEFAULT 'draft' CHECK(status IN ('draft','saved')),
  receipt_action_id BIGINT REFERENCES procurement_action_items(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX return_intakes_drafts_idx ON return_intakes(created_by,updated_at DESC) WHERE status='draft';
ALTER TABLE marketplace_returns DROP CONSTRAINT marketplace_returns_channel_check;
ALTER TABLE marketplace_returns ADD CONSTRAINT marketplace_returns_channel_check CHECK(channel IN ('wb','ozon','avito','saby','other'));
ALTER TABLE marketplace_returns ADD COLUMN intake_id UUID REFERENCES return_intakes(id);
ALTER TABLE marketplace_returns ADD COLUMN channel_comment TEXT NOT NULL DEFAULT '';
ALTER TABLE marketplace_returns ADD COLUMN receipt_action_id BIGINT REFERENCES procurement_action_items(id) ON DELETE SET NULL;
CREATE INDEX marketplace_returns_receipt_action_idx ON marketplace_returns(receipt_action_id) WHERE receipt_action_id IS NOT NULL;
CREATE INDEX marketplace_returns_intake_idx ON marketplace_returns(intake_id) WHERE intake_id IS NOT NULL;

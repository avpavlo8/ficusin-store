-- Server-resolved product/order cards; message text and binary attachments stay compatible.
ALTER TABLE support_messages ADD COLUMN IF NOT EXISTS reference JSONB;
ALTER TABLE support_messages ADD CONSTRAINT support_messages_reference_kind CHECK (
 reference IS NULL OR (jsonb_typeof(reference)='object' AND reference->>'kind' IN ('product','order') AND length(reference->>'id') BETWEEN 1 AND 40)
);

CREATE TABLE IF NOT EXISTS procurement_named_plan_drafts (
    id BIGSERIAL PRIMARY KEY,
    title TEXT NOT NULL,
    payload JSONB NOT NULL,
    created_by BIGINT REFERENCES customers(id) ON DELETE SET NULL,
    updated_by BIGINT REFERENCES customers(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS procurement_named_plan_drafts_active_updated
    ON procurement_named_plan_drafts(updated_at DESC, id DESC) WHERE deleted_at IS NULL;

INSERT INTO procurement_named_plan_drafts(title, payload, created_by, updated_by, created_at, updated_at)
SELECT 'Сохранённый черновик', payload, customer_id, customer_id, updated_at, updated_at
FROM procurement_plan_drafts;

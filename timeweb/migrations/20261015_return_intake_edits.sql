-- Atomic document edits retain both the prior document and physical unit facts.
CREATE TABLE return_intake_edits (
 id BIGSERIAL PRIMARY KEY,
 intake_id UUID NOT NULL REFERENCES return_intakes(id) ON DELETE CASCADE,
 revision BIGINT NOT NULL,
 request_data JSONB NOT NULL,
 before_data JSONB NOT NULL,
 after_data JSONB NOT NULL,
 before_items JSONB NOT NULL,
 after_items JSONB NOT NULL,
 created_by BIGINT REFERENCES customers(id) ON DELETE SET NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
 UNIQUE(intake_id,revision)
);

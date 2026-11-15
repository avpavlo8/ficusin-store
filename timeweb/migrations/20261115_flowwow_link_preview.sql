-- Read-only observations are deliberately separate from executable links/jobs.
CREATE TABLE flowwow_link_preview (
 singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK(singleton),
 captured_at TIMESTAMPTZ NOT NULL,
 payload JSONB NOT NULL CHECK(jsonb_typeof(payload)='object')
);

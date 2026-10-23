-- In-flight legacy checks must be requested again; existing sessions are unchanged.
ALTER TABLE call_checks ADD COLUMN IF NOT EXISTS browser_proof_hash TEXT;

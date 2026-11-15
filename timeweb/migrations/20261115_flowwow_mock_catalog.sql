-- Isolated emulator state only. Never used by the live Flowwow adapter.
CREATE TABLE flowwow_mock_catalog (
 offer_id TEXT PRIMARY KEY,
 payload JSONB NOT NULL CHECK(jsonb_typeof(payload)='object')
);

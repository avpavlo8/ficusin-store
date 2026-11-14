-- Sign-in and registration use verified phone calls. Neither password_hash
-- nor email_verified participates in authentication or authorization anymore.
-- Preserve original values independently of customer/account deletion.
LOCK TABLE customers IN ACCESS EXCLUSIVE MODE;
CREATE SCHEMA IF NOT EXISTS legacy_archive;
CREATE TABLE legacy_archive.customer_password_fields AS
 SELECT id AS customer_id,password_hash,email_verified,created_at,updated_at,
  CURRENT_TIMESTAMP AS archived_at
 FROM customers;
ALTER TABLE legacy_archive.customer_password_fields ADD PRIMARY KEY(customer_id);
COMMENT ON TABLE legacy_archive.customer_password_fields IS
 'Original retired password and unused email-verification fields; phone-call authentication and customer identity are unchanged';
ALTER TABLE customers DROP COLUMN password_hash,DROP COLUMN email_verified;

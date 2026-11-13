-- Native Saby publication uses the shared immutable request/lease machinery.
-- No queued job is promoted and no vendor request is emitted by this migration.
ALTER TABLE catalog_editor_remote_links
 DROP CONSTRAINT IF EXISTS catalog_editor_remote_links_channel_check;
ALTER TABLE catalog_editor_remote_links ADD CONSTRAINT catalog_editor_remote_links_channel_check
 CHECK(channel IN ('wb','ozon','saby'));

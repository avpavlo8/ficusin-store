-- There is no supported outbound Saby reservation/write-off implementation.
-- The old switch was never read: toggling it changed no worker or stock flow.
-- Keep stock_movements as the active order audit journal, and keep Saby's real
-- inbound stock synchronization. Remove only the misleading owner setting.
DELETE FROM settings WHERE key='saby.stock_enabled';

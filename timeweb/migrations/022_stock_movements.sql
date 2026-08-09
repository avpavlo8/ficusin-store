-- Журнал движений товара по настоящему складу.
--
-- Резерв ставится при заказе, списание — при отгрузке: так растение сразу
-- перестаёт быть доступным и другим покупателям, и продавцу в магазине, но
-- с остатка уходит лишь тогда, когда действительно уехало.
--
-- Пока запись остатков в СБИС выключена, журнал — единственное, что
-- происходит. Движения копятся со статусом «не отправлено», и по ним видно,
-- что магазин собирался сделать со складом. Сверить это с действительностью
-- дешевле, чем разбирать последствия неверного списания на живом складе.
CREATE TABLE IF NOT EXISTS stock_movements (
  id BIGSERIAL PRIMARY KEY,
  order_id BIGINT REFERENCES orders(id) ON DELETE SET NULL,
  variant_id BIGINT REFERENCES product_variants(id) ON DELETE SET NULL,
  -- Номенклатура СБИС на момент движения. Храним копией: товар могут
  -- отвязать от СБИС, а журнал должен остаться читаемым.
  saby_id TEXT NOT NULL DEFAULT '',
  kind TEXT NOT NULL,
  quantity INTEGER NOT NULL,
  status TEXT NOT NULL DEFAULT 'pending',
  reason TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  sent_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS stock_movements_order_idx ON stock_movements (order_id);
CREATE INDEX IF NOT EXISTS stock_movements_status_idx ON stock_movements (status, created_at);

-- Выключатель на время проверок: по умолчанию магазин настоящий склад не
-- трогает, только записывает намерения.
INSERT INTO settings (key, value) VALUES ('saby.stock_enabled', '0')
ON CONFLICT (key) DO NOTHING;

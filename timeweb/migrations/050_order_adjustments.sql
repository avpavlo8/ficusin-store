-- Частичные возвраты и доплаты по изменяемым заказам.
--
-- Платёж остаётся фактом поступления денег. Возврат хранится отдельной
-- проводкой: тогда заказ может состоять из нескольких успешных платежей и
-- нескольких частичных возвратов, а баланс всегда восстанавливается из
-- истории, а не из одного строкового статуса.
CREATE TABLE IF NOT EXISTS payment_refunds (
  id BIGSERIAL PRIMARY KEY,
  order_id BIGINT NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
  payment_id BIGINT NOT NULL REFERENCES payments(id) ON DELETE RESTRICT,
  provider TEXT NOT NULL DEFAULT 'yookassa',
  idempotence_key TEXT NOT NULL UNIQUE,
  amount NUMERIC(10,2) NOT NULL CHECK (amount > 0),
  reason TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'succeeded',
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS payment_refunds_order_idx ON payment_refunds(order_id);
CREATE INDEX IF NOT EXISTS payment_refunds_payment_idx ON payment_refunds(payment_id);

-- До этой миграции полный возврат помечал сам платёж как refunded и терял
-- отдельный факт возврата. Переносим такую историю в новую таблицу и снова
-- считаем исходный платёж успешным поступлением денег. Итоговый статус заказа
-- не меняем: новый расчёт баланса при следующем действии выставит его заново.
INSERT INTO payment_refunds(
  order_id, payment_id, idempotence_key, amount, reason, status
)
SELECT p.order_id, p.id, 'legacy-refund-' || p.id::TEXT, p.amount,
       'Перенесён полный возврат из прежней схемы', 'succeeded'
FROM payments p
WHERE p.status = 'refunded'
ON CONFLICT(idempotence_key) DO NOTHING;

UPDATE payments
SET status = 'paid', updated_at = CURRENT_TIMESTAMP
WHERE status = 'refunded';

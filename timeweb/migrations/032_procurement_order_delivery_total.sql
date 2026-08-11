-- Логистика вводится общей суммой за инвойс. Стоимость одной телеги хранится
-- только для обратной совместимости с ранее рассчитанными поставками.
ALTER TABLE procurement_orders
  ADD COLUMN IF NOT EXISTS delivery_to_moscow_rub NUMERIC(14,2) NOT NULL DEFAULT 0;

UPDATE procurement_orders AS orders SET delivery_to_moscow_rub =
  orders.trolley_cost_rub * COALESCE(NULLIF((
    SELECT COUNT(DISTINCT NULLIF(lines.load_unit, ''))
    FROM procurement_order_lines AS lines
    WHERE lines.procurement_order_id = orders.id AND lines.match_status = 'confirmed'
  ), 0), 1)
WHERE orders.delivery_to_moscow_rub = 0 AND orders.trolley_cost_rub > 0;

-- Дополнительная точность нужна, чтобы сумма долей после умножения на
-- количество сходилась с введённой стоимостью доставки практически до копейки.
ALTER TABLE procurement_order_lines
  ALTER COLUMN trolley_delivery_unit_rub TYPE NUMERIC(18,6),
  ALTER COLUMN ryazan_delivery_unit_rub TYPE NUMERIC(18,6),
  ALTER COLUMN unit_cost_rub TYPE NUMERIC(18,6);

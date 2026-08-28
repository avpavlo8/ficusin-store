-- Последняя цена карточки маркетплейса, прочитанная непосредственно перед
-- подготовкой изменения цен. Без неё интерфейс мог показать только новую
-- цену и ставил прочерк в колонке «Было».
ALTER TABLE procurement_channel_products
  ADD COLUMN IF NOT EXISTS current_price NUMERIC(14, 2),
  ADD COLUMN IF NOT EXISTS current_base_price NUMERIC(14, 2);

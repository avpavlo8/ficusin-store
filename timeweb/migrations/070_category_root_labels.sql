-- Корневые разделы должны называться одинаково в каталоге, фильтрах и
-- редакторе. Технические icon-токены остаются отдельным полем и никогда не
-- являются частью видимого названия.
UPDATE categories
SET name = CASE slug
  WHEN 'plants' THEN 'Растения'
  WHEN 'soil' THEN 'Грунты'
  WHEN 'fertilizer' THEN 'Удобрения'
  WHEN 'pots' THEN 'Кашпо'
  WHEN 'accessories' THEN 'Аксессуары'
  ELSE name
END,
updated_at = CURRENT_TIMESTAMP
WHERE slug IN ('plants', 'soil', 'fertilizer', 'pots', 'accessories');

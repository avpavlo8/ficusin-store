-- Зеркало фотографий: наши копии снимков, которые приходят из СБИС.
--
-- Ключ здесь — сама ссылка поставщика, а не строка product_media. Обмен с
-- СБИС удаляет и заново создаёт строки медиа при каждой синхронизации; будь
-- зеркало привязано к ним, магазин качал бы одни и те же снимки по кругу.
CREATE TABLE IF NOT EXISTS media_mirror (
  source_url TEXT PRIMARY KEY,
  -- Пустые ссылки означают, что перенос ещё не удался: причина и число
  -- попыток рядом, чтобы битый снимок не занимал очередь вечно.
  card_url TEXT,
  large_url TEXT,
  attempts INTEGER NOT NULL DEFAULT 0,
  failure TEXT NOT NULL DEFAULT '',
  mirrored_at TIMESTAMPTZ,
  checked_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Очередь ищет ненайденные и давние неудачи, поэтому смотрит по времени.
CREATE INDEX IF NOT EXISTS media_mirror_checked_idx
  ON media_mirror (checked_at);

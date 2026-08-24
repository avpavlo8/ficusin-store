-- Старые AI-обложки были записаны одним исходным WebP одновременно как
-- card_url и large_url. Переводим их в обычную очередь зеркалирования:
-- фоновый photo worker создаст отдельные JPEG 600px и 1200px и атомарно
-- обновит media_mirror. До завершения обработки каталог продолжит отдавать
-- исходную ссылку, поэтому миграция не создаёт окно с пустыми фотографиями.
UPDATE product_media AS media
SET object_key = mirror.large_url
FROM media_mirror AS mirror
WHERE media.object_key = mirror.source_url
  AND mirror.source_url LIKE 'ai://catalog-cover/%'
  AND mirror.card_url = mirror.large_url
  AND mirror.large_url LIKE 'https://s3.twcstorage.ru/%';

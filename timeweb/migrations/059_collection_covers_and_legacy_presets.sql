-- Restore the curated storefront collection rail that existed before the
-- database became authoritative, and make its artwork editable in admin.
ALTER TABLE collections
  ADD COLUMN IF NOT EXISTS cover_url TEXT NOT NULL DEFAULT '';

INSERT INTO collections (slug,title,note,sort_order,is_active,mode,rules,cover_url)
VALUES
 ('dark','Для тёмной комнаты','Растения для мест вдали от окна',10,1,'dynamic','[{"attribute":"light_level","operator":"eq","value":"low_light"}]','/assets/redesign/collection-dark-4k.webp'),
 ('easy','Неприхотливые','Простят забытый полив',20,1,'dynamic','[{"attribute":"care_level","operator":"eq","value":"easy"}]','/assets/redesign/collection-easy-4k.webp'),
 ('pets','Безопасно для питомцев','Не токсичны для кошек и собак',30,1,'dynamic','[{"attribute":"pet_safety","operator":"eq","value":"safe"}]','/assets/redesign/collection-pets-4k.webp'),
 ('bathroom','Для ванной','Любят влажный воздух',40,1,'dynamic','[{"attribute":"placement","operator":"contains","value":"bathroom"}]','/assets/redesign/filters/bathroom-wall-v2.webp'),
 ('office','Для офиса','Подходят для рабочего пространства',50,1,'dynamic','[{"attribute":"placement","operator":"contains","value":"office"}]','/assets/redesign/filters/office-wall-v2.webp'),
 ('tall','Вырастает высоким','Эффектные растения для пола',60,1,'dynamic','[{"attribute":"height_cm","operator":"gte","value":80}]','/assets/redesign/filters/tall-wall-v2.webp'),
 ('compact','Компактные','Для полок и небольших пространств',70,1,'dynamic','[{"attribute":"height_cm","operator":"lte","value":40}]','/assets/redesign/filters/compact-wall-v2.webp'),
 ('rare','Редкий полив','Не требуют частого полива',80,1,'dynamic','[{"attribute":"watering","operator":"eq","value":"rare"}]','/assets/redesign/filters/rare-water-wall-v2.webp'),
 ('bedroom','Для спальни','Спокойная зелень для отдыха',90,1,'dynamic','[{"attribute":"placement","operator":"contains","value":"bedroom"}]','/assets/redesign/filters/bedroom-wall-v2.webp')
ON CONFLICT (slug) DO UPDATE SET
 title=EXCLUDED.title,
 note=EXCLUDED.note,
 sort_order=EXCLUDED.sort_order,
 is_active=EXCLUDED.is_active,
 mode=EXCLUDED.mode,
 rules=EXCLUDED.rules,
 cover_url=CASE WHEN BTRIM(collections.cover_url)='' THEN EXCLUDED.cover_url ELSE collections.cover_url END,
 updated_at=CURRENT_TIMESTAMP;

-- The old seed used another slug for the pet-safe collection. Keep the row
-- for historical links but hide the duplicate from the storefront.
UPDATE collections SET is_active=0
WHERE slug='pet-safe' AND EXISTS (SELECT 1 FROM collections WHERE slug='pets');

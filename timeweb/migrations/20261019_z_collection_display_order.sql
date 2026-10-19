-- Homepage order requested by the owner after reviewing the live collections.
-- The remaining 16 reviewed collections retain their relative order.
UPDATE collections collection
SET sort_order=position.sort_order,updated_at=CURRENT_TIMESTAMP
FROM (VALUES
 ('desk',10),('fruiting',20),('pets',30),('easy',40),('dark',50),
 ('rare',60),('flowering',70),('sunny-window',80),('trailing',90),
 ('bathroom',100),('tall',110),('compact',120),('indoor-bonsai',130),
 ('gifts',140),('unusual',150),('variegated',160),('vertical',170),
 ('home-trees',180),('bonsai',190)
) position(slug,sort_order)
WHERE collection.slug=position.slug;

-- Correct an old supplier description that calls deciduous Pseudolarix an
-- evergreen indoor plant. RHS and NC State both identify it as a deciduous
-- outdoor conifer: https://www.rhs.org.uk/plants/211487/chrysolarix-amabilis/details
-- https://plants.ces.ncsu.edu/plants/pseudolarix-amabilis/common-name/golden-pine/
-- Exact product identity and old-text digest preserve later owner edits.
UPDATE products
SET description = 'Лжелиственница золотистая — листопадное хвойное дерево для выращивания на открытом воздухе. Мягкая хвоя осенью желтеет и опадает, а весной отрастает снова. В формате бонсай дерево проходит естественный зимний покой; корни в небольшой плошке нужно защитить от сильного промерзания.',
    updated_at = CURRENT_TIMESTAMP
WHERE product_code = 64
  AND name = 'Бонсай Лжелиственница'
  AND status = 'published'
  AND md5(description) = '3c8b0951d9dc9ddf85be475883a0839b';

# Five plant groups: safe identity preparation

`20261012_zzzzz_confirmed_merge_latin.sql` narrows two existing Latin labels to the exact named cultivars listed by RHS. Each update requires the existing product code, published plant card, exact current name and Latin, **and the complete expected SKU ↔ СБИС X-code set**. Changed or incomplete live data leave the row untouched.

| Source card | Latin correction | Primary source |
|---|---|---|
| SKU 486, Фикус Бенджамина «Грин Кинки» | `Ficus benjamina` → `Ficus benjamina 'Green Kinky'` | [RHS cultivar entry](https://www.rhs.org.uk/plants/317750/ficus-benjamina-green-kinky/details) |
| SKU 506/563, Нефролепис «Грин Леди» | `Nephrolepis 'Green Lady'` → `Nephrolepis exaltata 'Green Lady'` | [RHS cultivar entry](https://www.rhs.org.uk/plants/304353/nephrolepis-exaltata-green-lady/details) |

This migration does **not** merge cards. It also does not unify the different descriptions, care guidance, PIM characteristics or photos. Those choices would remove or overwrite differing information:

- Замиокулькас «Зензи» #110 and source SKU798 disagree on `light_level` (`low_light`/`diffused`), care text and passport. RHS lists Zenzi as a trade name associated with `Z. zamiifolia 'Hansoti 13'`; the supplier cultivar identity has not been verified for each SKU, so this package leaves both Latin values at species level.
- Грин Кинки #198/486 and Бамбино #205/689/690 disagree on `care_level` (`easy`/`medium`) and copy. The owner should choose a common truthful care classification. Bambino's two-SKU source also has one card-level photo with no confirmed size assignment.
- Дипсис Арека #91/92/565 already has a common Latin (`Chrysalidocarpus lutescens`, accepted by [Kew POWO](https://powo.science.kew.org/taxon/urn:lsid:ipni.org:names:665962-1)). Source SKU565 has different descriptions and care text. Preserve them until edited into one shared card text.
- Грин Леди #156/506/563 differs on `watering` (`frequent`/`moderate`) and copy. The two-SKU source has a card-level photo with no confirmed size assignment.

After deployment, run the owner merge preview separately for each group, resolve any remaining exact guard, and perform the merge only when all source media and external IDs are attributed to the correct variant. No marketplace mappings, stock, price or photo should be inferred from another size.

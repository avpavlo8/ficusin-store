#!/usr/bin/env python3
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


def replace_once(path: str, old: str, new: str) -> None:
    target = ROOT / path
    text = target.read_text(encoding="utf-8")
    # Check the final form first because some replacements intentionally keep
    # the old line as a prefix (for example adding an import after another).
    if new in text:
        print("already", path)
        return
    if old in text:
        target.write_text(text.replace(old, new, 1), encoding="utf-8")
        print("patched", path)
        return
    raise RuntimeError(f"fragment not found in {path}: {old[:160]!r}")


def replace_between(path: str, start: str, end: str, replacement: str) -> None:
    target = ROOT / path
    text = target.read_text(encoding="utf-8")
    left = text.find(start)
    if left < 0:
        if replacement in text:
            print("already", path)
            return
        raise RuntimeError(f"start marker not found in {path}: {start!r}")
    right = text.find(end, left)
    if right < 0:
        raise RuntimeError(f"end marker not found in {path}: {end!r}")
    target.write_text(text[:left] + replacement + text[right:], encoding="utf-8")
    print("replaced block", path)


# Repair duplicate imports created by the first non-idempotent version of this
# helper before applying anything else.
for relative, line in (
    ("frontend/src/AdminPim.tsx", 'import { VariantMediaManager } from "./VariantMediaManager";\n'),
    ("frontend/src/AdminCatalog.tsx", 'import { CollectionsV2 } from "./AdminCollectionsV2";\n'),
):
    target = ROOT / relative
    source = target.read_text(encoding="utf-8")
    duplicate = line + line
    if duplicate in source:
        target.write_text(source.replace(duplicate, line), encoding="utf-8")
        print("deduplicated", relative)

# SKU media UI: the backend already stores variant_id on product_media. Expose
# the same upload/primary/delete controls directly in the SKU editor.
replace_once(
    "frontend/src/AdminPim.tsx",
    'import { api, money } from "./adminShared";\n',
    'import { api, money } from "./adminShared";\nimport { VariantMediaManager } from "./VariantMediaManager";\n',
)
replace_once(
    "frontend/src/AdminPim.tsx",
    '      <h4 className="wide">Фото SKU</h4><div className="wide"><p className="admin-hint">{draft.images.length ? `${draft.images.length} фото привязано к этому SKU. На PDP они имеют приоритет над общими фото товара.` : "Собственных фото нет — PDP использует общую галерею товара."}</p></div>',
    '      <h4 className="wide">Фото SKU</h4>{selectedId === "new" ? <p className="wide admin-hint">Сначала сохраните SKU — после этого можно загрузить его фотографии.</p> : <VariantMediaManager variantId={draft.id} sku={draft.sku} onChanged={() => { void load(); }} onError={onError} />}',
)

# Replace the old hand-only collection editor with the full CRUD/rules UI.
replace_once(
    "frontend/src/AdminCatalog.tsx",
    'import { AttributeManager } from "./AdminPim";\n',
    'import { AttributeManager } from "./AdminPim";\nimport { CollectionsV2 } from "./AdminCollectionsV2";\n',
)
replace_once(
    "frontend/src/AdminCatalog.tsx",
    'import type { AdminCollection, Category, Product, ReviewModerationItem } from "./adminTypes";\n',
    'import type { Category, Product, ReviewModerationItem } from "./adminTypes";\n',
)
replace_between(
    "frontend/src/AdminCatalog.tsx",
    '// Collections are assembled by hand:',
    'export function Products(',
    'export function Collections({ onError }: { onError: (value: string) => void }) {\n  return <CollectionsV2 onError={onError} />;\n}\n\n',
)

# Public catalogue: manual membership stays in collection_products; dynamic
# membership is evaluated from current PIM values at read time.
replace_once(
    "backend/internal/catalog/postgres.go",
    """\t\tCOALESCE((SELECT ARRAY_AGG(collection.slug ORDER BY collection.sort_order,collection.id)\n\t\t\tFROM collection_products member JOIN collections collection ON collection.id=member.collection_id AND collection.is_active=1\n\t\t\tWHERE member.product_id=product.id),ARRAY[]::TEXT[]),""",
    """\t\tCOALESCE((SELECT ARRAY_AGG(collection.slug ORDER BY collection.sort_order,collection.id)\n\t\t\tFROM collections collection\n\t\t\tWHERE collection.is_active=1 AND ((collection.mode='manual' AND EXISTS(SELECT 1 FROM collection_products member WHERE member.collection_id=collection.id AND member.product_id=product.id))\n\t\t\t\tOR (collection.mode='dynamic' AND collection_rules_match_product(product.id,collection.rules)))),ARRAY[]::TEXT[]),""",
)
replace_once(
    "backend/internal/catalog/postgres.go",
    """\t\tSELECT collection.slug,collection.title,collection.note,COUNT(member.product_id)::INTEGER\n\t\tFROM collections collection JOIN collection_products member ON member.collection_id=collection.id\n\t\tJOIN products product ON product.id=member.product_id AND product.status='published'\n\t\tWHERE collection.is_active=1 GROUP BY collection.id HAVING COUNT(member.product_id)>0\n\t\tORDER BY collection.sort_order,collection.id""",
    """\t\tSELECT collection.slug,collection.title,collection.note,COUNT(product.id)::INTEGER\n\t\tFROM collections collection\n\t\tJOIN products product ON product.status='published' AND ((collection.mode='manual' AND EXISTS(SELECT 1 FROM collection_products member WHERE member.collection_id=collection.id AND member.product_id=product.id))\n\t\t\tOR (collection.mode='dynamic' AND collection_rules_match_product(product.id,collection.rules)))\n\t\tWHERE collection.is_active=1 GROUP BY collection.id HAVING COUNT(product.id)>0\n\t\tORDER BY collection.sort_order,collection.id""",
)

# Compact visual treatment for rule rows and SKU photos.
css_path = ROOT / "frontend/src/styles/admin.css"
css = css_path.read_text(encoding="utf-8")
marker = "/* catalog-v2 collection rules and SKU media */"
if marker not in css:
    css += """

/* catalog-v2 collection rules and SKU media */
.collection-editor-v2 { display: grid; gap: 18px; }
.collection-rules { display: grid; gap: 12px; }
.collection-rule-row { display: grid; grid-template-columns: minmax(190px, 1.4fr) minmax(130px, .8fr) minmax(160px, 1fr) auto; gap: 8px; align-items: center; }
.admin-collection-preview { display: grid; gap: 4px; padding: 12px; border: 1px solid var(--line, #d9ddd6); border-radius: 12px; }
.variant-media-manager { display: grid; gap: 10px; }
.variant-media-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(120px, 1fr)); gap: 10px; }
.variant-media-grid article { display: grid; gap: 6px; padding: 8px; border: 1px solid var(--line, #d9ddd6); border-radius: 12px; }
.variant-media-grid img { width: 100%; aspect-ratio: 1; object-fit: cover; border-radius: 8px; }
.variant-media-grid article > div { display: flex; flex-wrap: wrap; gap: 6px; align-items: center; }
@media (max-width: 760px) { .collection-rule-row { grid-template-columns: 1fr; } }
"""
    css_path.write_text(css, encoding="utf-8")
    print("appended catalog-v2 admin styles")
else:
    print("already frontend/src/styles/admin.css")

print("catalogue v2 final follow-up applied")

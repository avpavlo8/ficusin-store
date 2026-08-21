#!/usr/bin/env python3
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


def replace(path: str, old: str, new: str) -> None:
    target = ROOT / path
    text = target.read_text(encoding="utf-8")
    if old not in text:
        if new in text:
            print("already", path)
            return
        raise RuntimeError(f"fragment not found in {path}: {old[:120]!r}")
    target.write_text(text.replace(old, new, 1), encoding="utf-8")
    print("patched", path)


def between(path: str, start: str, end: str, body: str) -> None:
    target = ROOT / path
    text = target.read_text(encoding="utf-8")
    left = text.find(start)
    right = text.find(end, left + len(start)) if left >= 0 else -1
    if left < 0 or right < 0:
        if body in text:
            print("already", path)
            return
        raise RuntimeError(f"markers not found in {path}: {start!r} .. {end!r}")
    target.write_text(text[:left] + body + text[right:], encoding="utf-8")
    print("replaced", path)


# Legacy product-form schema carries scope so PRODUCT and SKU fields cannot be
# accidentally rendered/saved in the same value map.
replace(
    "backend/internal/admin/admin.go",
    "Audience     string `json:\"audience\"`\n\tRequired",
    "Audience     string `json:\"audience\"`\n\tScope        string `json:\"scope\"`\n\tRequired",
)
replace(
    "backend/internal/admin/manage.go",
    "Unit: definition.Unit, Options: options, Audience: definition.Audience,\n\t\t\tRequired:",
    "Unit: definition.Unit, Options: options, Audience: definition.Audience, Scope: definition.Scope,\n\t\t\tRequired:",
)
replace(
    "frontend/src/adminTypes.ts",
    'unit: string; options: string[]; audience: "customer" | "technical";\n  required:',
    'unit: string; options: string[]; audience: "customer" | "technical"; scope: "product" | "variant";\n  required:',
)

# Only the owner may mutate category topology; managers still see the tree and
# edit products/SKUs through the product section.
replace(
    "frontend/src/AdminCatalog.tsx",
    'import { PageHeading, api, money, sabyFieldLabels, statusLabels } from "./adminShared";',
    'import { PageHeading, api, money, sabyFieldLabels, statusLabels } from "./adminShared";\nimport { AttributeManager } from "./AdminPim";',
)
replace(
    "frontend/src/AdminCatalog.tsx",
    'export function Categories({ canEdit, onError }: { canEdit: boolean; onError: (value: string) => void }) {',
    'export function Categories({ canEdit, owner, onError }: { canEdit: boolean; owner: boolean; onError: (value: string) => void }) {',
)
replace(
    "frontend/src/AdminCatalog.tsx",
    'return <><PageHeading eyebrow="Структура каталога" title="Категории" text="Три уровня: раздел, группа и вид растения. Категории с товарами защищены от удаления." />',
    'return <><PageHeading eyebrow="Структура каталога" title="Категории и атрибуты" text="Дерево любой глубины. Категории с товарами или дочерними узлами защищены от удаления." />',
)
replace(
    "frontend/src/AdminCatalog.tsx",
    '{orderedItems.filter((item) => depth(item) < 2).map((item) => <option value={item.id} key={item.id}>{`${"— ".repeat(depth(item))}${item.name}`}</option>)}',
    '{orderedItems.map((item) => <option value={item.id} key={item.id}>{`${"— ".repeat(depth(item))}${item.name}`}</option>)}',
)
replace(
    "frontend/src/AdminCatalog.tsx",
    '    })}</tbody></table></div>\n  </>;',
    '    })}</tbody></table></div>\n    {owner && <AttributeManager categories={items} onError={onError} />}\n  </>;',
)
replace(
    "frontend/src/AdminPage.tsx",
    '{section === "categories" && <Categories canEdit={can("products.edit")} onError={setError} />}',
    '{section === "categories" && <Categories canEdit={data.role === "owner"} owner={data.role === "owner"} onError={setError} />}',
)

# Product card edits PRODUCT data only. SKU price/stock/dimensions/integrations
# are handled by the dedicated variant editor below it.
replace(
    "frontend/src/AdminCatalogDialogs.tsx",
    'import { Dialog, api, money, sabyFieldLabels } from "./adminShared";',
    'import { Dialog, api, money, sabyFieldLabels } from "./adminShared";\nimport { VariantsEditor } from "./AdminPim";',
)
replace(
    "frontend/src/AdminCatalogDialogs.tsx",
    'attributes: validAttributes(schema, form.attributes || {}),\n        externalIds: form.externalIds,\n        ...(form.sabyFields.includes("stock") ? {} : { stock: form.stock }),',
    'attributes: validAttributes(schema.filter((item) => item.scope === "product"), form.attributes || {}),',
)
# Remove old variant fields from the Product PATCH payload.
replace(
    "frontend/src/AdminCatalogDialogs.tsx",
    'featured: form.featured, image: form.image, priceMinor: Math.round(form.price * 100),\n        variantLabel: form.variantLabel, heightCm: form.heightCm, potDiameterCm: form.potDiameterCm,\n        packageLengthCm: form.packageLengthCm, packageWidthCm: form.packageWidthCm,\n        packageHeightCm: form.packageHeightCm, packageWeightGrams: form.packageWeightGrams,\n        wholesaleMinQty: form.wholesaleMinQty, catalogSection: form.catalogSection, categoryId: form.categoryId,\n        plantKind: form.plantKind || "", lightLevel: form.lightLevel || "", watering: form.watering || "",\n        heightClass: form.heightClass || "", careLevel: form.careLevel || "", placement: form.placement || "",\n        petSafety: form.petSafety || "", growthHabit: form.growthHabit || "",',
    'featured: form.featured, image: form.image, catalogSection: form.catalogSection, categoryId: form.categoryId,',
)
replace(
    "frontend/src/AdminCatalogDialogs.tsx",
    '<AttributeFields schema={schema.filter((item) => item.audience === "customer")} values={form.attributes || {}}',
    '<AttributeFields schema={schema.filter((item) => item.scope === "product" && item.audience === "customer")} values={form.attributes || {}}',
)

variant_block = '''<VariantsEditor productId={product.id} categoryId={form.categoryId} onError={onError} />\n    '''
between(
    "frontend/src/AdminCatalogDialogs.tsx",
    '    <h3 className="product-form-heading wide">Продажа</h3>',
    '    <label>Статус<select value={form.status}',
    variant_block,
)

# Keep top-level Saby source selection as a PRODUCT sync policy, not an external
# identity editor. It remains after variants so the manager can control stock
# import without forging a SKU.
replace(
    "frontend/src/AdminCatalogDialogs.tsx",
    '<VariantsEditor productId={product.id} categoryId={form.categoryId} onError={onError} />\n    <label>Статус',
    '<VariantsEditor productId={product.id} categoryId={form.categoryId} onError={onError} />\n    {form.sabyId && <div className="wide admin-field"><span className="admin-field-label">Что берём из СБИС</span><div className="sync-options">{Object.entries(sabyFieldLabels).map(([field, label]) => <label key={field}><input type="checkbox" checked={form.sabyFields.includes(field)} onChange={(event) => setForm({ ...form, sabyFields: event.target.checked ? [...form.sabyFields, field] : form.sabyFields.filter((item) => item !== field) })} /><span><strong>{label}</strong></span></label>)}</div></div>}\n    <label>Статус',
)

# The product PATCH still sends Saby policy explicitly.
replace(
    "frontend/src/AdminCatalogDialogs.tsx",
    'passport: form.passport, importantWarnings: form.importantWarnings,\n        sabyFields: form.sabyFields,\n        attributes:',
    'passport: form.passport, importantWarnings: form.importantWarnings,\n        sabyFields: form.sabyFields,\n        attributes:',
)

# No native +/- spinner in compact PIM numeric controls.
css = ROOT / "frontend/src/index.css"
text = css.read_text(encoding="utf-8")
addition = r'''
/* Catalogue PIM ---------------------------------------------------------- */
.admin-pim{margin-top:32px;border-top:1px solid var(--line,#e7e3dc);padding-top:28px}.pim-header,.variant-editor-head{display:flex;align-items:flex-start;justify-content:space-between;gap:20px}.pim-header h2,.variant-editor-head h3{margin:0 0 6px}.pim-header p,.variant-editor-head p{margin:0;color:#716b63}.pim-columns{display:grid;grid-template-columns:minmax(220px,.72fr) minmax(480px,1.6fr);gap:20px;margin-top:22px}.pim-definitions,.pim-schema,.pim-filters,.admin-pim-editor,.variant-detail{border:1px solid #e4dfd7;border-radius:18px;background:#fff;padding:18px}.pim-definitions>div{display:flex;align-items:center;border-bottom:1px solid #eee9e2}.pim-definitions>div>button:first-child{flex:1;text-align:left;border:0;background:none;padding:12px 0;display:grid;gap:3px}.pim-definitions code,.pim-assignment code{font-size:11px;color:#817a70}.pim-definitions small,.pim-assignment small{color:#8a8379}.pim-assignment{display:grid;grid-template-columns:minmax(170px,.7fr) minmax(360px,1.3fr);gap:14px;padding:12px 0;border-bottom:1px solid #eee9e2}.pim-assignment>div:first-child{display:grid;gap:3px}.pim-assignment.inherited{background:#faf9f6}.pim-flags{display:flex;align-items:center;gap:9px;flex-wrap:wrap}.pim-flags label{font-size:12px;display:flex;align-items:center;gap:4px}.tiny-number{width:64px!important}.pim-add{display:flex;gap:8px;margin:10px 0 14px}.pim-add select{flex:1}.pim-filters{margin-top:20px}.variant-editor{margin-top:8px;border-top:1px solid #e4dfd7;padding-top:20px}.variant-detail{margin-top:14px}.variant-summary{display:flex;justify-content:space-between;margin-bottom:14px}.admin-table tr.selected{background:#f5f2eb}.admin-pim-editor{margin-top:16px}.admin-pim input[type=number],.variant-editor input[type=number],.product-form input[type=number]{-moz-appearance:textfield;appearance:textfield}.admin-pim input[type=number]::-webkit-inner-spin-button,.admin-pim input[type=number]::-webkit-outer-spin-button,.variant-editor input[type=number]::-webkit-inner-spin-button,.variant-editor input[type=number]::-webkit-outer-spin-button,.product-form input[type=number]::-webkit-inner-spin-button,.product-form input[type=number]::-webkit-outer-spin-button{-webkit-appearance:none;margin:0}@media(max-width:900px){.pim-columns{grid-template-columns:1fr}.pim-assignment{grid-template-columns:1fr}.pim-header,.variant-editor-head{flex-direction:column}}
'''
if "/* Catalogue PIM" not in text:
    css.write_text(text + addition, encoding="utf-8")
    print("patched frontend/src/index.css")

print("catalogue UI follow-up applied")

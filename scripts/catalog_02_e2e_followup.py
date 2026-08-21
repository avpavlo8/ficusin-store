#!/usr/bin/env python3
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


def replace(path: str, old: str, new: str) -> None:
    target = ROOT / path
    text = target.read_text(encoding="utf-8")
    if old in text:
        target.write_text(text.replace(old, new), encoding="utf-8")
        print("patched", path, repr(old), "->", repr(new))
        return
    if new in text:
        print("already", path, repr(new))
        return
    raise RuntimeError(f"fragment not found in {path}: {old!r}")


# Shared storefront fixture: public card identity is product_code while cart
# identity is SKU. Saby identifiers do not participate in either contract.
helper = ROOT / "e2e/tests/helpers.ts"
text = helper.read_text(encoding="utf-8")
text = text.replace('id: "saby-1",\n  name: "Аглаонема Мария",', 'id: "1",\n  sku: "1",\n  name: "Аглаонема Мария",')
text = text.replace('id: "saby-2",\n  name: "Фикус Бенджамина",', 'id: "2",\n  sku: "2",\n  name: "Фикус Бенджамина",')
text = text.replace('id: "saby-3",\n  name: "Монстера Делициоза",', 'id: "3",\n  sku: "3",\n  name: "Монстера Делициоза",')
text = text.replace('variants: [{ id: 1, sku: "X100",', 'variants: [{ id: 1, sku: "1",')
helper.write_text(text, encoding="utf-8")
print("patched e2e/tests/helpers.ts")

# Customer-facing E2E files used the old public Saby/slug identity. Replace it
# only in storefront tests; procurement keeps Saby IDs because there they are
# intentionally external integration identifiers.
for relative in (
    "e2e/tests/core-flows.spec.ts",
    "e2e/tests/delivery-checkout.spec.ts",
    "e2e/tests/mobile-layout.spec.ts",
    "e2e/tests/storefront.spec.ts",
):
    path = ROOT / relative
    source = path.read_text(encoding="utf-8")
    source = source.replace("saby-1", "1").replace("saby-2", "2").replace("saby-3", "3")
    source = source.replace('id: "X100"', 'id: "1"')
    path.write_text(source, encoding="utf-8")
    print("patched", relative)

# PDP owner fixture keeps Saby as an external mapping, but public code/SKU are
# our numeric IDs. The admin database row id remains 1.
pdp = ROOT / "e2e/tests/pdp-admin.spec.ts"
source = pdp.read_text(encoding="utf-8")
source = source.replace('slug: "saby-1",', 'slug: "1",')
source = source.replace('sku: "FIC-000001",', 'sku: "1",')
source = source.replace('page.goto("/product/saby-1")', 'page.goto("/product/1")')
pdp.write_text(source, encoding="utf-8")
print("patched e2e/tests/pdp-admin.spec.ts")

# A test or a partially configured category can legitimately return an empty
# schema object. Rendering the product form must not remount/crash while the
# owner is typing; an absent array is simply an empty schema.
replace(
    "frontend/src/AdminCatalogDialogs.tsx",
    "if (active) setSchema(data.attributes);",
    "if (active) setSchema(data.attributes || []);",
)

print("catalogue E2E identity follow-up applied")

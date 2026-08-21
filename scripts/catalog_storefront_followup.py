#!/usr/bin/env python3
from pathlib import Path
ROOT=Path(__file__).resolve().parents[1]

def replace(path,old,new):
    target=ROOT/path;text=target.read_text(encoding='utf-8')
    if old not in text:
        if new in text: print('already',path);return
        raise RuntimeError(f'fragment not found {path}: {old[:100]!r}')
    target.write_text(text.replace(old,new,1),encoding='utf-8');print('patched',path)

def between(path,start,end,body):
    target=ROOT/path;text=target.read_text(encoding='utf-8');left=text.find(start);right=text.find(end,left+len(start)) if left>=0 else -1
    if left<0 or right<0:
        if body in text: print('already',path);return
        raise RuntimeError(f'markers not found {path}')
    target.write_text(text[:left]+body+'\n\n'+text[right:],encoding='utf-8');print('replaced',path)

# Only owner-configured active storefront filters become public filter facets.
# The configured title overrides the generic attribute name; category-scoped
# filters apply to descendants through the same ancestor CTE.
replace(
 'backend/internal/catalog/postgres.go',
 "SELECT effective.code,effective.name,effective.unit,effective.sort_order,effective.is_filterable,effective.is_badge,value.value\n\t\t\t\tFROM effective JOIN product_attribute_values value",
 "SELECT effective.code,COALESCE((SELECT filter.title FROM catalog_filters filter WHERE filter.attribute_id=effective.id AND filter.is_active AND (filter.category_id IS NULL OR EXISTS(SELECT 1 FROM ancestors ancestor WHERE ancestor.id=filter.category_id)) ORDER BY filter.category_id NULLS LAST,filter.sort_order,filter.id LIMIT 1),effective.name),effective.unit,effective.sort_order,effective.is_filterable,effective.is_badge,value.value\n\t\t\t\tFROM effective JOIN product_attribute_values value"
)
replace(
 'backend/internal/catalog/postgres.go',
 "WHERE effective.value_scope='product' AND NOT effective.is_excluded AND (effective.is_filterable OR effective.is_badge)\n\t\t\t\tUNION ALL",
 "WHERE effective.value_scope='product' AND NOT effective.is_excluded AND (effective.is_badge OR (effective.is_filterable AND EXISTS(SELECT 1 FROM catalog_filters filter WHERE filter.attribute_id=effective.id AND filter.is_active AND (filter.category_id IS NULL OR EXISTS(SELECT 1 FROM ancestors ancestor WHERE ancestor.id=filter.category_id)))))\n\t\t\t\tUNION ALL"
)
replace(
 'backend/internal/catalog/postgres.go',
 "SELECT effective.code,effective.name,effective.unit,effective.sort_order,effective.is_filterable,effective.is_badge,value.value\n\t\t\t\tFROM effective JOIN variant_attribute_values value",
 "SELECT effective.code,COALESCE((SELECT filter.title FROM catalog_filters filter WHERE filter.attribute_id=effective.id AND filter.is_active AND (filter.category_id IS NULL OR EXISTS(SELECT 1 FROM ancestors ancestor WHERE ancestor.id=filter.category_id)) ORDER BY filter.category_id NULLS LAST,filter.sort_order,filter.id LIMIT 1),effective.name),effective.unit,effective.sort_order,effective.is_filterable,effective.is_badge,value.value\n\t\t\t\tFROM effective JOIN variant_attribute_values value"
)
replace(
 'backend/internal/catalog/postgres.go',
 "WHERE effective.value_scope='variant' AND NOT effective.is_excluded AND (effective.is_filterable OR effective.is_badge)",
 "WHERE effective.value_scope='variant' AND NOT effective.is_excluded AND (effective.is_badge OR (effective.is_filterable AND EXISTS(SELECT 1 FROM catalog_filters filter WHERE filter.attribute_id=effective.id AND filter.is_active AND (filter.category_id IS NULL OR EXISTS(SELECT 1 FROM ancestors ancestor WHERE ancestor.id=filter.category_id)))))"
)

# Facets are calculated from the current search/category population rather
# than the whole catalogue, so impossible/empty values never appear.
old_facets='''  const facets = useMemo(() => {\n    const result = new Map<string, { name: string; unit?: string; values: Set<string> }>();\n    products.forEach((product) => product.filterAttributes?.filter((attribute) => attribute.filterable).forEach((attribute) => {\n      const facet = result.get(attribute.code) || { name: attribute.name, unit: attribute.unit, values: new Set<string>() };\n      const values = Array.isArray(attribute.value) ? attribute.value : [attribute.value]; values.forEach((value) => facet.values.add(String(value))); result.set(attribute.code, facet);\n    }));\n    return [...result.entries()].filter(([, facet]) => facet.values.size > 1);\n  }, [products]);\n\n  // Четыре главных фильтра из утверждённого макета существуют независимо\n  // от того, настроил ли менеджер дублирующие динамические характеристики.\n  const catalogFacets = useMemo(() => {\n    const values = (pick: (product: Product) => string | undefined) => new Set(products.map(pick).filter((value): value is string => Boolean(value)));\n    return [\n      ["__light", { name: "Освещённость", values: values((product) => product.lightLevel) }],\n      ["__watering", { name: "Полив", values: values((product) => product.watering) }],\n      ["__care", { name: "Уход", values: values((product) => product.careLevel) }],\n      ["__diameter", { name: "Размеры", unit: "см", values: values((product) => product.size.match(/D\\s*(\\d+)/i)?.[1]) }],\n      ["__pets", { name: "Для питомцев", values: values((product) => product.petSafety) }],\n    ] as Array<[string, { name: string; unit?: string; values: Set<string> }]>;\n  }, [products]);'''
new_facets='''  const facetPopulation = useMemo(() => {\n    if (searching) return found;\n    if (category == null) return found;\n    return found.filter((item) => inBranch(item.categoryId, category));\n  }, [found, searching, category, inBranch]);\n\n  const facets = useMemo(() => {\n    const result = new Map<string, { name: string; unit?: string; values: Set<string> }>();\n    facetPopulation.forEach((product) => product.filterAttributes?.filter((attribute) => attribute.filterable).forEach((attribute) => {\n      const facet = result.get(attribute.code) || { name: attribute.name, unit: attribute.unit, values: new Set<string>() };\n      const values = Array.isArray(attribute.value) ? attribute.value : [attribute.value];\n      values.forEach((value) => { if (value !== null && value !== undefined && String(value) !== "") facet.values.add(String(value)); });\n      result.set(attribute.code, facet);\n    }));\n    return [...result.entries()].filter(([, facet]) => facet.values.size > 0);\n  }, [facetPopulation]);'''
replace('frontend/src/StorefrontPage.tsx',old_facets,new_facets)

# Filtering is now uniformly attribute-driven; no special React codes are a
# second source of truth.
replace(
 'frontend/src/StorefrontPage.tsx',
 '''      if (code === "__diameter") return product.size.match(/D\\s*(\\d+)/i)?.[1] === selected;\n      if (code === "__light") return product.lightLevel === selected;\n      if (code === "__watering") return product.watering === selected;\n      if (code === "__care") return product.careLevel === selected;\n      if (code === "__pets") return product.petSafety === selected;\n      return product.filterAttributes?.some((attribute) => attribute.code === code && (Array.isArray(attribute.value) ? attribute.value.map(String).includes(selected) : String(attribute.value) === selected));''',
 '''      return product.filterAttributes?.some((attribute) => attribute.code === code && (Array.isArray(attribute.value) ? attribute.value.map(String).includes(selected) : String(attribute.value) === selected));'''
)
replace(
 'frontend/src/StorefrontPage.tsx',
 '{catalogFacets.map(([code, facet]) => <CatalogDropdown',
 '{facets.slice(0, 6).map(([code, facet]) => <CatalogDropdown'
)

print('storefront attribute filters applied')

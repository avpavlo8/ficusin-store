#!/usr/bin/env python3
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


def replace_once(path: str, old: str, new: str) -> None:
    target = ROOT / path
    text = target.read_text(encoding="utf-8")
    if old in text:
        target.write_text(text.replace(old, new, 1), encoding="utf-8")
        print("patched", path)
        return
    if new in text:
        print("already", path)
        return
    raise RuntimeError(f"fragment not found in {path}: {old[:120]!r}")


def remove_between(path: str, start: str, end: str) -> None:
    target = ROOT / path
    text = target.read_text(encoding="utf-8")
    left = text.find(start)
    if left < 0:
        print("already", path)
        return
    right = text.find(end, left)
    if right < 0:
        raise RuntimeError(f"end marker not found in {path}: {end!r}")
    target.write_text(text[:left] + text[right:], encoding="utf-8")
    print("removed dead block", path)


# First-pass compile fixes. They are already present after the generated commit,
# but keeping these idempotent makes this last helper safe for one final run.
replace_once(
    "frontend/src/AdminPim.tsx",
    'api<{ attributes: EffectiveAttribute[] }>(`/api/v1/admin/categories/${categoryId}/effective-attributes`).then((data) => setSchema(data.attributes.filter((item) => item.scope === "variant" && item.active && !item.excluded)).catch((error) => onError(error.message));',
    'api<{ attributes: EffectiveAttribute[] }>(`/api/v1/admin/categories/${categoryId}/effective-attributes`).then((data) => setSchema(data.attributes.filter((item) => item.scope === "variant" && item.active && !item.excluded))).catch((error) => onError(error.message));',
)
remove_between(
    "frontend/src/AdminCatalogDialogs.tsx",
    "function ExternalMappings(",
    "export function ProductDialog",
)

# React 19 lint rejects synchronous state writes from effects. Assignment rows
# are reset by a data-sensitive key instead; async API callbacks remain effects.
replace_once(
    "frontend/src/AdminPim.tsx",
    '  useEffect(() => setDraft(item), [item]);\n',
    '',
)
replace_once(
    "frontend/src/AdminPim.tsx",
    '  const [categoryId, setCategoryId] = useState<number | null>(categories[0]?.id || null);\n  const [effective, setEffective] = useState<EffectiveAttribute[]>([]);',
    '  const [categoryId, setCategoryId] = useState<number | null>(categories[0]?.id || null);\n  const selectedCategoryId = categoryId ?? categories[0]?.id ?? null;\n  const [effective, setEffective] = useState<EffectiveAttribute[]>([]);',
)
replace_once(
    "frontend/src/AdminPim.tsx",
    '  const loadEffective = useCallback(() => {\n    if (!categoryId) { setEffective([]); return; }\n    api<{ attributes: EffectiveAttribute[] }>(`/api/v1/admin/categories/${categoryId}/effective-attributes`).then((data) => setEffective(data.attributes)).catch((error) => onError(error.message));\n  }, [categoryId, onError]);',
    '  const loadEffective = useCallback(() => {\n    if (!selectedCategoryId) return Promise.resolve();\n    return api<{ attributes: EffectiveAttribute[] }>(`/api/v1/admin/categories/${selectedCategoryId}/effective-attributes`).then((data) => setEffective(data.attributes)).catch((error) => onError(error.message));\n  }, [selectedCategoryId, onError]);',
)
replace_once(
    "frontend/src/AdminPim.tsx",
    '  useEffect(() => { if (!categoryId && categories[0]) setCategoryId(categories[0].id); }, [categories, categoryId]);\n',
    '',
)
replace_once(
    "frontend/src/AdminPim.tsx",
    '    if (!categoryId || !addAttributeId) return;\n    try {\n      await api(`/api/v1/admin/categories/${categoryId}/attributes/${addAttributeId}`',
    '    if (!selectedCategoryId || !addAttributeId) return;\n    try {\n      await api(`/api/v1/admin/categories/${selectedCategoryId}/attributes/${addAttributeId}`',
)
replace_once(
    "frontend/src/AdminPim.tsx",
    '<select value={categoryId || ""} onChange={(event) => setCategoryId(Number(event.target.value) || null)}>',
    '<select value={selectedCategoryId || ""} onChange={(event) => setCategoryId(Number(event.target.value) || null)}>',
)
replace_once(
    "frontend/src/AdminPim.tsx",
    '<AttributeAssignmentRow item={item} categoryId={categoryId!} onSaved={loadEffective} onError={onError} key={item.id} />',
    '<AttributeAssignmentRow item={item} categoryId={selectedCategoryId!} onSaved={loadEffective} onError={onError} key={`${selectedCategoryId}:${item.id}:${item.inherited}:${item.sourceCategoryId ?? "local"}:${item.required}:${item.filterable}:${item.showOnPdp}:${item.keyCharacteristic}:${item.badge}:${item.sortOrder}:${item.summaryPosition ?? ""}:${item.showInCharacteristics}:${item.excluded}`} />',
)

# When a product temporarily has no category, hide the cached schema in render
# rather than synchronously clearing state from an effect.
replace_once(
    "frontend/src/AdminPim.tsx",
    '  useEffect(() => { if (!categoryId) { setSchema([]); return; } api<{ attributes: EffectiveAttribute[] }>(`/api/v1/admin/categories/${categoryId}/effective-attributes`).then((data) => setSchema(data.attributes.filter((item) => item.scope === "variant" && item.active && !item.excluded))).catch((error) => onError(error.message)); }, [categoryId, onError]);',
    '  useEffect(() => { if (!categoryId) return; void api<{ attributes: EffectiveAttribute[] }>(`/api/v1/admin/categories/${categoryId}/effective-attributes`).then((data) => setSchema(data.attributes.filter((item) => item.scope === "variant" && item.active && !item.excluded))).catch((error) => onError(error.message)); }, [categoryId, onError]);',
)
replace_once(
    "frontend/src/AdminPim.tsx",
    '  const variantAttributes = schema.filter((item) => item.audience === "customer");\n  const technicalAttributes = schema.filter((item) => item.audience === "technical");\n  const completeness = schema.length ? Math.round(schema.filter((item) => !item.required || draft.attributes[item.code] !== undefined && draft.attributes[item.code] !== null && draft.attributes[item.code] !== "").length / schema.length * 100) : 100;',
    '  const visibleSchema = categoryId ? schema : [];\n  const variantAttributes = visibleSchema.filter((item) => item.audience === "customer");\n  const technicalAttributes = visibleSchema.filter((item) => item.audience === "technical");\n  const completeness = visibleSchema.length ? Math.round(visibleSchema.filter((item) => !item.required || draft.attributes[item.code] !== undefined && draft.attributes[item.code] !== null && draft.attributes[item.code] !== "").length / visibleSchema.length * 100) : 100;',
)

print("catalogue compile/lint follow-up applied")

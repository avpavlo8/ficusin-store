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

print("catalogue compile follow-up applied")

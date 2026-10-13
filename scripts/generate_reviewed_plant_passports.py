#!/usr/bin/env python3
"""Generate a guarded SQL migration and source ledger from reviewed plant care JSON.

Usage: python3 scripts/generate_reviewed_plant_passports.py input.json \
  --sql timeweb/migrations/20261013_d_reviewed_plant_passports.sql \
  --sources docs/plant-passport-sources-2026-10-13.md
"""

import argparse
import json
import re
from pathlib import Path

TOPICS = ("humidity", "temperature", "soil", "repotting", "fertilizer", "growthRate")
PLACEHOLDERS = ("готовится", "добавьте рекомендацию", "уточняется", "информация для")


def quote(value: str) -> str:
    return "'" + value.replace("'", "''") + "'"


def validate(rows: object) -> list[dict]:
    if not isinstance(rows, list) or not rows:
        raise ValueError("Expected a nonempty JSON array")
    seen = set()
    for index, row in enumerate(rows, 1):
        if not isinstance(row, dict):
            raise ValueError(f"Row {index}: expected object")
        code, name = row.get("code"), row.get("name")
        if type(code) is not int or code <= 0 or code in seen:
            raise ValueError(f"Row {index}: code must be a unique positive integer")
        seen.add(code)
        if not isinstance(name, str) or not name.strip():
            raise ValueError(f"Row {index}: exact current name required")
        if not isinstance(row.get("taxon"), str) or not row["taxon"].strip():
            raise ValueError(f"Row {index}: reviewed taxon or group required")
        urls = row.get("sourceUrls")
        if not isinstance(urls, list) or not urls or not all(
            isinstance(url, str) and re.fullmatch(r"https://[^\s]+", url) for url in urls
        ):
            raise ValueError(f"Row {index}: at least one HTTPS source URL required")
        passport = row.get("passport")
        if not isinstance(passport, dict) or not passport or not set(passport) <= set(TOPICS):
            raise ValueError(f"Row {index}: passport must contain only supported topics")
        for key, value in passport.items():
            if not isinstance(value, str) or len(value.strip()) < 25:
                raise ValueError(f"Row {index}/{key}: substantive text required")
            if any(placeholder in value.casefold() for placeholder in PLACEHOLDERS):
                raise ValueError(f"Row {index}/{key}: placeholder text is not content")
    return rows


def make_sql(rows: list[dict]) -> str:
    values = []
    for row in rows:
        passport = json.dumps(row["passport"], ensure_ascii=False, separators=(",", ":"))
        values.append(f" ({row['code']},{quote(row['name'])},{quote(passport)}::jsonb)")
    return "\n".join(
        [
            "-- Reviewed botanical content. Source URLs and taxon decisions are recorded in the paired sources document.",
            "-- Fill only blank passport topics on the exact published plant card; preserve owner edits.",
            "CREATE TEMP TABLE reviewed_plant_passports (",
            " product_code bigint PRIMARY KEY, expected_name text NOT NULL, passport jsonb NOT NULL",
            ") ON COMMIT DROP;",
            "INSERT INTO reviewed_plant_passports VALUES\n" + ",\n".join(values) + ";",
            "UPDATE products AS p SET",
            " plant_passport = COALESCE(p.plant_passport,'{}'::jsonb) || (",
            "  SELECT COALESCE(jsonb_object_agg(topic.key,topic.value),'{}'::jsonb)",
            "  FROM jsonb_each(c.passport) AS topic(key,value)",
            "  WHERE BTRIM(COALESCE(p.plant_passport->>topic.key,''))=''",
            " ),",
            " updated_at = CURRENT_TIMESTAMP",
            "FROM reviewed_plant_passports AS c",
            "WHERE p.product_code=c.product_code AND p.name=c.expected_name",
            " AND p.catalog_section='plants' AND p.status='published'",
            " AND EXISTS (SELECT 1 FROM jsonb_each(c.passport) AS topic(key,value)",
            "             WHERE BTRIM(COALESCE(p.plant_passport->>topic.key,''))='');",
            "",
        ]
    )


def make_sources(rows: list[dict]) -> str:
    lines = ["# Источники для заполнения паспортов растений", "", "Код и название сверены с живой опубликованной карточкой до миграции. Тексты заполняют только пустые темы.", ""]
    for row in rows:
        lines.extend([
            f"## #{row['code']} — {row['name']}", "",
            f"Таксон или группа: {row['taxon']}", "",
            "Темы: " + ", ".join(row["passport"]), "",
            "Источники:", "",
            *[f"- {url}" for url in row["sourceUrls"]], "",
        ])
    return "\n".join(lines)


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("input", type=Path)
    parser.add_argument("--sql", type=Path, required=True)
    parser.add_argument("--sources", type=Path, required=True)
    args = parser.parse_args()
    rows = validate(json.loads(args.input.read_text(encoding="utf-8")))
    args.sql.write_text(make_sql(rows), encoding="utf-8")
    args.sources.write_text(make_sources(rows), encoding="utf-8")
    print(f"Generated {len(rows)} guarded product updates")


if __name__ == "__main__":
    main()

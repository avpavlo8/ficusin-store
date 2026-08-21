#!/usr/bin/env python3
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
marker = ROOT / "docs" / "catalog-model-v2.md"
marker.write_text("""# Catalogue model v2\n\nTarget model: product card (SPU) + sellable variant (SKU).\n\n- Product card owns category and shared content.\n- Variant SKU owns price, stock, dimensions, variant attributes, media overrides and external mappings.\n- Public product URLs use Ficusin product codes only.\n- Saby/WB/Ozon identifiers are external mappings, never catalogue identity.\n- Reviews belong to the product and retain the purchased variant SKU.\n""", encoding="utf-8")
print("catalogue rewrite helper: smoke marker written")

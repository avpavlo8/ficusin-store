# Catalogue model v2

The catalogue uses explicit Ficusin-owned identities.

- PRODUCT: `products.id` internal FK + immutable numeric `product_code` for `/product/{code}`.
- SKU: `product_variants.id` internal FK + immutable numeric `sku` for the sellable unit.
- Cart keys are SKUs. Order rows snapshot PRODUCT id, variant id, SKU, label and core variant characteristics.
- Saby/WB/Ozon identifiers are external mappings, never catalogue identity.
- Product and variant attributes are separate; enum options are normalized.
- Reviews belong to PRODUCT and remember the purchased variant/SKU.

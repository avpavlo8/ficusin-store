const categoryNames = new Intl.Collator("ru", { sensitivity: "base", numeric: true });

// Storefront visitors look up names alphabetically. Manual sortOrder remains
// available for the administrative tree, but must not scramble public menus.
export function orderStorefrontCategories<
  T extends { id: number; name: string; parentId: number | null; sortOrder: number },
>(items: T[]): T[] {
  return [...items].sort((a, b) => {
    const rootOrder = a.parentId === null && b.parentId === null ? a.sortOrder - b.sortOrder : 0;
    return rootOrder || categoryNames.compare(a.name, b.name) || a.id - b.id;
  });
}

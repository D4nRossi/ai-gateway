/**
 * filterByText — case-insensitive substring filter sobre múltiplas chaves do objeto.
 * Retorna a lista inalterada quando a query está vazia (preserva referência para
 * o React não re-renderizar à toa).
 *
 * Uso: const visible = filterByText(items, search, (it) => [it.name, it.tier]);
 */
export function filterByText<T>(
  items: T[],
  search: string,
  fields: (item: T) => Array<string | number | null | undefined>,
): T[] {
  const q = search.trim().toLowerCase();
  if (!q) return items;
  return items.filter((it) =>
    fields(it).some((v) => v != null && String(v).toLowerCase().includes(q)),
  );
}

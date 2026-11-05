/** The item to select after `id` is removed: the next one, else the previous. */
export function neighbourOf<T extends { id: string }>(items: T[], id: string): string | null {
  const i = items.findIndex((m) => m.id === id)
  if (i === -1) return null
  return items[i + 1]?.id ?? items[i - 1]?.id ?? null
}

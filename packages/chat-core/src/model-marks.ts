/** How many ids one `GET /models/marks` takes. */
export const MAX_MODEL_MARKS = 50

/**
 * The models replies name that the model list no longer has (an administrator
 * removed or turned them off), sorted and without repeats, so the query key is
 * the same whatever order the replies came in. Empty until the list is known:
 * before then every model looks missing.
 */
export function missingModels(ids: readonly (string | null | undefined)[], listed: ReadonlySet<string> | undefined): string[] {
  if (!listed) return []
  const missing = new Set<string>()
  for (const id of ids) if (id && !listed.has(id)) missing.add(id)
  return [...missing].sort().slice(0, MAX_MODEL_MARKS)
}

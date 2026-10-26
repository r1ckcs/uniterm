// Item 50: inside group "POP Cordovil", "OLT-ZTE-C600-CORDOVIL" shows as
// "OLT-ZTE-C600" — the group name already says Cordovil, and the middle cut
// would otherwise keep exactly that and hide the model. Display only: the
// full name stays in title / aria-label / ⌘K.

const SEP = '[-_. /:]'
const fold = (s: string) => s.normalize('NFD').replace(/[̀-ͯ]/g, '').toLowerCase()
const escape = (s: string) => s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')

// "POP Cordovil" → "cordovil"; "POP Rio das Ostras" → "rio das ostras".
export function groupKey(groupName: string): string {
  return fold(groupName || '').replace(/^pop\s+/, '').trim()
}

// The name without the group's token (whole segment, with one separator).
// Unchanged when the token is absent or the name is only the group.
export function stripGroupToken(name: string, key: string): string {
  if (!key || !name) return name
  // Words of the key may be joined by any separator in the name.
  const token = key.split(/\s+/).map(escape).join(SEP)
  const folded = fold(name)
  const re = new RegExp(`(^|${SEP})${token}(?=${SEP}|$)`)
  const m = re.exec(folded)
  if (!m) return name
  let start = m.index
  let end = m.index + m[0].length
  // Drop the separator before the token; at the very start, the one after.
  if (m[1] === '' && /[-_. /:]/.test(name[end] ?? '')) end++
  const out = name.slice(0, start) + name.slice(end)
  return /[^-_. /:]/.test(out) ? out : name
}

// Labels for the connections of one group. A name whose stripped label would
// collide with another's keeps its full name (both of them).
export function groupLabels(names: { id: string; name: string }[], groupName: string): Map<string, string> {
  const key = groupKey(groupName)
  const stripped = names.map((n) => ({ id: n.id, name: n.name, label: stripGroupToken(n.name, key) }))
  const count = new Map<string, number>()
  for (const s of stripped) count.set(fold(s.label), (count.get(fold(s.label)) ?? 0) + 1)
  const out = new Map<string, string>()
  for (const s of stripped) out.set(s.id, (count.get(fold(s.label)) ?? 0) > 1 ? s.name : s.label)
  return out
}

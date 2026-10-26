// Keyboard model of the sidebar connection tree (item 45): the rows in
// render order, and what each key does. WAI-ARIA tree pattern with a single
// tab stop (roving tabindex). Pure data so it can be tested.

export interface TreeRow {
  key: string
  kind: 'group' | 'conn' | 'action'
  label: string
  level: number
  parentKey?: string
  expanded?: boolean // groups only
}

export interface TreeRowInfo {
  row: TreeRow
  index: number
  posinset: number
  setsize: number
}

// posinset/setsize among siblings (same parent), for aria-posinset/-setsize.
export function treeRowInfo(rows: TreeRow[]): Map<string, TreeRowInfo> {
  const byParent = new Map<string, TreeRow[]>()
  for (const r of rows) {
    const p = r.parentKey ?? ''
    if (!byParent.has(p)) byParent.set(p, [])
    byParent.get(p)!.push(r)
  }
  const out = new Map<string, TreeRowInfo>()
  rows.forEach((row, index) => {
    const sibs = byParent.get(row.parentKey ?? '')!
    out.set(row.key, { row, index, posinset: sibs.indexOf(row) + 1, setsize: sibs.length })
  })
  return out
}

export type TreeAction =
  | { type: 'focus'; key: string }
  | { type: 'expand'; key: string }
  | { type: 'collapse'; key: string }
  | { type: 'activate'; key: string }
  | { type: 'menu'; key: string }
  | { type: 'none' }

const fold = (s: string) => s.normalize('NFD').replace(/[̀-ͯ]/g, '').toLowerCase()

// Next row (after the focused one, wrapping) whose label starts with the
// typed text. One repeated letter cycles through the rows with that letter.
export function typeaheadKey(rows: TreeRow[], focusKey: string | null, typed: string): string | null {
  if (!typed || !rows.length) return null
  const q = fold(typed)
  const same = q.split('').every((c) => c === q[0])
  const start = Math.max(0, rows.findIndex((r) => r.key === focusKey))
  const offset = same || q.length === 1 ? 1 : 0
  for (let i = 0; i < rows.length; i++) {
    const r = rows[(start + offset + i) % rows.length]
    const label = fold(r.label)
    if (label.startsWith(q) || (same && label.startsWith(q[0]))) return r.key
  }
  return null
}

export function treeKeyAction(rows: TreeRow[], focusKey: string | null, e: Pick<KeyboardEvent, 'key' | 'shiftKey'>): TreeAction {
  if (!rows.length) return { type: 'none' }
  const idx = rows.findIndex((r) => r.key === focusKey)
  const cur = idx >= 0 ? rows[idx] : undefined
  const at = (i: number): TreeAction => ({ type: 'focus', key: rows[i].key })
  switch (e.key) {
    case 'ArrowDown':
      return at(idx < 0 ? 0 : Math.min(rows.length - 1, idx + 1))
    case 'ArrowUp':
      return at(idx < 0 ? 0 : Math.max(0, idx - 1))
    case 'Home':
      return at(0)
    case 'End':
      return at(rows.length - 1)
    case 'ArrowRight':
      if (!cur) return at(0)
      if (cur.kind !== 'group') return { type: 'none' }
      if (!cur.expanded) return { type: 'expand', key: cur.key }
      return rows[idx + 1]?.parentKey === cur.key ? at(idx + 1) : { type: 'none' }
    case 'ArrowLeft':
      if (!cur) return at(0)
      if (cur.kind === 'group' && cur.expanded) return { type: 'collapse', key: cur.key }
      return cur.parentKey ? { type: 'focus', key: cur.parentKey } : { type: 'none' }
    case 'Enter':
      return cur ? { type: 'activate', key: cur.key } : { type: 'none' }
    case 'ContextMenu':
      return cur ? { type: 'menu', key: cur.key } : { type: 'none' }
    case 'F10':
      return cur && e.shiftKey ? { type: 'menu', key: cur.key } : { type: 'none' }
  }
  return { type: 'none' }
}

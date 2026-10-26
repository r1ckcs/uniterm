// Texts for deleting connections and groups (item 42): the prompt names what
// goes, and the undo notice names what went. Pure, so the wording is tested
// without a DOM.
import type { PendingDelete } from '../stores/connectionStore'

type T = (key: string, params?: Record<string, string | number>) => string

// "A", "A e B", "A, B e C", "A, B, C e mais 9".
export function nameList(t: T, names: string[]): string {
  const [a = '', b = '', c = ''] = names
  if (names.length <= 1) return a
  if (names.length === 2) return t('conn.list2', { a, b })
  if (names.length === 3) return t('conn.list3', { a, b, c })
  return t('conn.listMore', { a, b, c, count: names.length - 3 })
}

export function connectionLabel(c: { name?: string; host?: string }): string {
  return c.name || c.host || ''
}

export function deleteConnectionsPrompt(t: T, conns: { name?: string; host?: string }[]): string {
  if (conns.length === 1) {
    const c = conns[0]
    const name = connectionLabel(c)
    return c.host && c.host !== name
      ? t('conn.deleteOne', { name, host: c.host })
      : t('conn.deleteOneNoHost', { name })
  }
  return t('conn.deleteMany', { count: conns.length, list: nameList(t, conns.map(connectionLabel)) })
}

export function deletedNotice(t: T, p: PendingDelete): string {
  if (p.kind === 'group') return t('conn.deletedGroup', { name: p.groupName ?? '', count: p.names.length })
  if (p.names.length === 1) return t('conn.deletedOne', { name: p.names[0] })
  return t('conn.deletedMany', { count: p.names.length, list: nameList(t, p.names) })
}

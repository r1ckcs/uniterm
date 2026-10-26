// ⌘1–9 / Ctrl+1–9 address SESSION tabs only (item 51): the start page and
// settings take no number (⌘T / ⌘N already open a start page), so the
// first session is ⌘1 and nine sessions get a digit, not eight.
import type { Tab } from '../types/workspace'

export function isNumberedTab(tab: Pick<Tab, 'type'>): boolean {
  return tab.type !== 'start' && tab.type !== 'settings'
}

// The tab a digit (1–9) switches to, or undefined.
export function tabForDigit(tabs: Tab[], digit: number): Tab | undefined {
  return tabs.filter(isNumberedTab)[digit - 1]
}

// Digit of each tab (1-based among session tabs), absent for the others
// and past the ninth.
export function tabDigits(tabs: Tab[]): Map<string, number> {
  const out = new Map<string, number>()
  let n = 0
  for (const t of tabs) {
    if (!isNumberedTab(t)) continue
    n++
    if (n <= 9) out.set(t.id, n)
  }
  return out
}

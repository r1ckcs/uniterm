import { describe, expect, it } from 'vitest'
import { isQuickSearchShortcut } from './quickSearch'

const ev = (key: string, m: Partial<Record<'metaKey' | 'ctrlKey' | 'shiftKey' | 'altKey', boolean>> = {}) =>
  ({ key, metaKey: false, ctrlKey: false, shiftKey: false, altKey: false, ...m })

describe('quick search shortcut', () => {
  it('is Cmd+K on macOS and leaves Ctrl+K to the shell', () => {
    expect(isQuickSearchShortcut(ev('k', { metaKey: true }), true)).toBe(true)
    expect(isQuickSearchShortcut(ev('K', { metaKey: true }), true)).toBe(true)
    expect(isQuickSearchShortcut(ev('k', { ctrlKey: true }), true)).toBe(false)
    expect(isQuickSearchShortcut(ev('k', { metaKey: true, shiftKey: true }), true)).toBe(false)
  })

  it('is Ctrl+Shift+P on Windows/Linux, never plain Ctrl+K', () => {
    expect(isQuickSearchShortcut(ev('P', { ctrlKey: true, shiftKey: true }), false)).toBe(true)
    expect(isQuickSearchShortcut(ev('k', { ctrlKey: true }), false)).toBe(false)
    expect(isQuickSearchShortcut(ev('p', { ctrlKey: true }), false)).toBe(false)
  })
})

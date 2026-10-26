import { describe, expect, it } from 'vitest'
import type { Tab } from '../types/workspace'
import { tabDigits, tabForDigit } from './tabShortcuts'
import ptBR from '../i18n/locales/pt-BR.json'
import en from '../i18n/locales/en.json'

const tab = (id: string, type: string) => ({ id, type, name: id }) as unknown as Tab
const tabs = [tab('start', 'start'), tab('a', 'terminal'), tab('set', 'settings'), tab('b', 'workspace'), tab('c', 'sftp')]

describe('⌘1–9 address session tabs only (item 51)', () => {
  it('the start page and settings take no number; ⌘1 is the first session', () => {
    expect([...tabDigits(tabs)]).toEqual([['a', 1], ['b', 2], ['c', 3]])
    expect(tabForDigit(tabs, 1)?.id).toBe('a')
    expect(tabForDigit(tabs, 3)?.id).toBe('c')
    expect(tabForDigit(tabs, 4)).toBeUndefined()
  })

  it('nine sessions get a digit, the tenth none', () => {
    const many = [tab('start', 'start'), ...Array.from({ length: 10 }, (_, i) => tab(`s${i}`, 'terminal'))]
    const d = tabDigits(many)
    expect(d.get('s8')).toBe(9)
    expect(d.has('s9')).toBe(false)
  })
})

describe('texts of item 51', () => {
  const raw = import.meta.glob(['../style.css', '../components/HostKeyChangedDialog.vue'], { query: '?raw', import: 'default', eager: true }) as Record<string, string>

  it('no "(s)" / "(ns)" pseudo-plurals left in pt-BR or en', () => {
    const bad = (b: Record<string, string>) => Object.entries(b)
      .filter(([k, v]) => k !== 'conn.expectColTimeout' && /[a-zà-ú]\((s|ns|es|ões)\)/i.test(v)).map(([k]) => k)
    expect(bad(ptBR as Record<string, string>)).toEqual([])
    expect(bad(en as Record<string, string>)).toEqual([])
  })

  it('shortcut texts take the platform modifier instead of a fixed Ctrl', () => {
    for (const k of ['terminal.broadcastCtrlHint', 'settings.showTabShortcutHintsDesc', 'settings.ctrlWheelZoom', 'settings.ctrlWheelZoomDesc']) {
      expect((ptBR as Record<string, string>)[k], k).not.toMatch(/Ctrl|Cmd/)
      expect((en as Record<string, string>)[k], k).not.toMatch(/Ctrl|Cmd/)
    }
  })

  it('reduced motion collapses transitions and animations', () => {
    expect(raw['../style.css']).toMatch(/@media \(prefers-reduced-motion: reduce\) \{[^]*?animation-duration: 0\.01ms !important;[^]*?transition-duration: 0\.01ms !important;/)
  })

  it('the downgrade notice has no side stripe', () => {
    const dlg = raw['../components/HostKeyChangedDialog.vue']
    expect(dlg).not.toMatch(/border-left:/)
    expect(dlg).toMatch(/\.hkc-downgrade \{[^}]*border: 1px solid var\(--error\);/)
  })
})

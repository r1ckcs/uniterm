import { describe, expect, it } from 'vitest'
import { terminalFontChoices, terminalLineHeight } from './terminalFonts'
import { formatFontFamily } from './formatFontFamily'

describe('terminal line height', () => {
  it('defaults to 1.0 when unset and clamps to 1.0–1.5 in 0.05 steps', () => {
    expect(terminalLineHeight(undefined)).toBe(1)
    expect(terminalLineHeight(1.1)).toBe(1.1)
    expect(terminalLineHeight(1.12)).toBe(1.1)
    expect(terminalLineHeight(0.8)).toBe(1)
    expect(terminalLineHeight(2)).toBe(1.5)
  })
})

describe('terminal font choices', () => {
  const bundled = { Name: 'JetBrains Mono Variable', IsMono: true }

  it('macOS: hidden faces dropped, SF Mono and Menlo pinned after the bundled font', () => {
    const got = terminalFontChoices([bundled, { Name: '.SF NS Mono', IsMono: true }, { Name: 'Andale Mono', IsMono: true }, { Name: 'Menlo', IsMono: true }, { Name: '.SF Arabic', IsMono: false }])
    expect(got.map(f => f.Name)).toEqual(['JetBrains Mono Variable', 'SF Mono', 'Menlo', 'Andale Mono'])
  })

  it('Windows: Cascadia pinned only when installed', () => {
    const got = terminalFontChoices([bundled, { Name: 'Consolas', IsMono: true }, { Name: 'Cascadia Mono', IsMono: true }])
    expect(got.map(f => f.Name)).toEqual(['JetBrains Mono Variable', 'Cascadia Mono', 'Consolas'])
  })

  it('SF Mono falls back to the system monospace face in the webview', () => {
    expect(formatFontFamily('SF Mono')).toBe('"SF Mono", ui-monospace, monospace')
  })
})

import { describe, expect, it } from 'vitest'
import { contrastRatio } from './utils/contrast'
import { getXtermTheme } from './composables/useTerminal'
import { resolveTerminalThemeName } from './composables/useTerminalTheme'
import { FOLLOW_APP_THEME } from './types/settings'

const css = Object.values(import.meta.glob('./style.css', { query: '?raw', import: 'default', eager: true }))[0] as string

// The custom properties a [data-theme="…"] block sets, as name → value.
function themeTokens(theme: string): Record<string, string> {
  const start = css.indexOf(`[data-theme="${theme}"] {`)
  expect(start, theme).toBeGreaterThan(-1)
  const body = css.slice(start, css.indexOf('\n}', start))
  return Object.fromEntries([...body.matchAll(/(--[\w-]+):\s*([^;]+);/g)].map(m => [m[1], m[2].trim()]))
}

// Text token → the surfaces it sits on. Normal text needs 4.5:1 (WCAG AA).
const TEXT_PAIRS: Array<[string, string[]]> = [
  ['--text-primary', ['--bg-base', '--bg-elevated', '--bg-surface', '--bg-sunken', '--bg-input', '--bg-hover', '--bg-active']],
  ['--text-secondary', ['--bg-base', '--bg-elevated', '--bg-surface', '--bg-sunken', '--bg-input', '--bg-hover']],
  ['--text-muted', ['--bg-base', '--bg-elevated', '--bg-surface', '--bg-sunken', '--bg-input']],
  ['--accent-text', ['--bg-base', '--bg-surface', '--bg-sunken', '--accent-subtle']],
  ['--on-accent', ['--accent']],
  ['--success', ['--bg-base', '--bg-surface', '--success-subtle']],
  ['--warning', ['--bg-base', '--bg-surface', '--warning-subtle']],
  ['--error', ['--bg-base', '--bg-surface', '--error-subtle']],
  ['--alert-text', ['--alert-card', '--alert-row', '--alert-badge']],
  ['--ok-text', ['--ok-badge', '--bg-surface']],
  ['--tag-olt', ['--bg-sunken', '--alert-row']],
  ['--tag-pe', ['--bg-sunken', '--alert-row']],
  ['--tag-mk', ['--bg-sunken', '--alert-row']],
  ['--tag-sw', ['--bg-sunken', '--alert-row']],
  ['--header-text', ['--header-bg', '--header-tab-active']],
  ['--header-text-2', ['--header-bg', '--header-tab-active']],
  ['--header-text-3', ['--header-bg', '--header-tab-active']],
]

describe('FlexTerm (D1c) themes', () => {
  it('style.css is readable', () => expect(css.length).toBeGreaterThan(1000))

  for (const theme of ['flexterm', 'flexterm-light']) {
    it(`${theme}: text reads at >= 4.5:1 on its surfaces`, () => {
      const tokens = themeTokens(theme)
      for (const [fg, bgs] of TEXT_PAIRS) {
        for (const bg of bgs) {
          expect(tokens[fg], `${theme} ${fg}`).toMatch(/^#[0-9a-f]{6}$/i)
          expect(tokens[bg], `${theme} ${bg}`).toMatch(/^#[0-9a-f]{6}$/i)
          expect(contrastRatio(tokens[fg], tokens[bg]), `${theme} ${fg} on ${bg}`).toBeGreaterThanOrEqual(4.5)
        }
      }
    })

    it(`${theme}: solid colors only, no shadows`, () => {
      const tokens = themeTokens(theme)
      // Translucency is only allowed for the loading scrim.
      const translucent = Object.entries(tokens).filter(([k, v]) => /rgba|transparent/.test(v) && k !== '--scrim')
      expect(translucent).toEqual([])
      for (const k of ['--shadow-sm', '--shadow-md', '--shadow-lg', '--el-box-shadow']) expect(tokens[k], k).toBe('none')
    })
  }

  it('header tabs paint text with solid tokens that the contrast test covers', () => {
    // The dropped (red) tab's shortcut hint uses the alert text color, and a
    // tab without a session gets a solid dimmer color, not opacity.
    expect(css).toMatch(/\.tab-item\.tab-down \.tab-shortcut \{\s*color: var\(--alert-text\);/)
    expect(css).toMatch(/\.tab-name\.tab-disconnected \{\s*opacity: 1;\s*color: var\(--header-text-3\);/)
  })

  it('the terminal follows the app theme with the matching palette', () => {
    expect(resolveTerminalThemeName(FOLLOW_APP_THEME, 'flexterm')).toBe('flexterm-dark')
    expect(resolveTerminalThemeName(FOLLOW_APP_THEME, 'flexterm-light')).toBe('flexterm-light')
    expect(resolveTerminalThemeName(FOLLOW_APP_THEME, 'deep-blue')).toBe('uniterm-dark')
    expect(resolveTerminalThemeName(FOLLOW_APP_THEME, 'light')).toBe('uniterm-light')
    expect(getXtermTheme('flexterm-dark').background).toBe(themeTokens('flexterm')['--bg-base'])
  })

  it('terminal text reads on the FlexTerm terminal backgrounds', () => {
    for (const name of ['flexterm-dark', 'flexterm-light']) {
      const th = getXtermTheme(name) as Record<string, string>
      expect(contrastRatio(th.foreground, th.background), name).toBeGreaterThanOrEqual(7)
      for (const k of ['red', 'green', 'yellow', 'blue', 'magenta', 'cyan', 'white', 'brightRed', 'brightGreen', 'brightYellow', 'brightBlue', 'brightMagenta', 'brightCyan', 'brightBlack']) {
        expect(contrastRatio(th[k], th.background), `${name} ${k}`).toBeGreaterThanOrEqual(4.5)
      }
    }
  })
})

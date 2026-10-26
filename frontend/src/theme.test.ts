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
  ['--on-danger', ['--danger-solid', '--danger-solid-hover']],
  // Broadcast: the band's text, the footer flag.
  ['--on-broadcast', ['--broadcast-band']],
  ['--broadcast-text', ['--bg-base', '--bg-elevated', '--bg-surface']],
  ['--success', ['--bg-base', '--bg-surface', '--success-subtle']],
  // --bg-elevated: the terminal status bar paints LOG / broadcast on it.
  ['--warning', ['--bg-base', '--bg-surface', '--bg-elevated', '--warning-subtle']],
  ['--error', ['--bg-base', '--bg-surface', '--bg-elevated', '--error-subtle']],
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

  for (const theme of ['flexterm', 'flexterm-light']) {
    it(`${theme}: connection palette icons read at >= 3:1 on lists, cards and the header`, () => {
      const tokens = themeTokens(theme)
      const surfaces = ['--bg-base', '--bg-elevated', '--bg-surface', '--bg-sunken', '--bg-active', '--alert-row', '--alert-card']
      for (const key of ['red', 'orange', 'amber', 'green', 'cyan', 'blue', 'purple', 'gray']) {
        for (const bg of surfaces) {
          expect(contrastRatio(tokens[`--conn-${key}`], tokens[bg]), `${theme} ${key} on ${bg}`).toBeGreaterThanOrEqual(3)
        }
        for (const bg of ['--header-bg', '--header-tab-active']) {
          expect(contrastRatio(tokens[`--conn-${key}-header`], tokens[bg]), `${theme} ${key} on ${bg}`).toBeGreaterThanOrEqual(3)
        }
      }
    })
  }

  for (const theme of ['flexterm', 'flexterm-light']) {
    it(`${theme}: broadcast has its own hue, framed at >= 3:1`, () => {
      const tokens = themeTokens(theme)
      for (const bg of ['--bg-base', '--bg-elevated', '--bg-surface']) {
        expect(contrastRatio(tokens['--broadcast'], tokens[bg]), `${theme} --broadcast on ${bg}`).toBeGreaterThanOrEqual(3)
      }
      for (const bg of ['--header-bg', '--header-tab-active']) {
        expect(contrastRatio(tokens['--broadcast-header'], tokens[bg]), `${theme} --broadcast-header on ${bg}`).toBeGreaterThanOrEqual(3)
      }
      // Not the action blue, the TELNET/OLT orange, the warning or the alert red.
      const taken = ['--accent', '--accent-text', '--tag-olt', '--conn-orange', '--warning', '--error', '--danger-solid']
        .map((k) => tokens[k]?.toLowerCase())
      for (const k of ['--broadcast', '--broadcast-text', '--broadcast-header', '--broadcast-band']) {
        expect(taken, `${theme} ${k}`).not.toContain(tokens[k].toLowerCase())
      }
    })
  }

  it('broadcast is drawn with the tested tokens: band, terminal frame, tab mark, footer', () => {
    const raw = (f: string) => Object.values(import.meta.glob(['./components/Panel.vue', './components/TabItem.vue', './components/TerminalStatusBar.vue'], { query: '?raw', import: 'default', eager: true }))
      .find((src) => (src as string).includes(f)) as string
    const panel = raw('panel-broadcast-band')
    expect(panel).toMatch(/\.panel-broadcasting::after \{[^}]*border: 2px solid var\(--broadcast\);/)
    expect(panel).toMatch(/\.panel-broadcast-band \{[^}]*background: var\(--broadcast-band\);\s*color: var\(--on-broadcast\);/)
    expect(raw('tab-broadcasting')).toMatch(/\.tab-item\.tab-broadcasting::after \{[^}]*border: 2px solid var\(--broadcast-header\);/)
    expect(raw('tsb-broadcast')).toMatch(/\.tsb-broadcast \{\s*color: var\(--broadcast-text\);/)
  })

  for (const theme of ['flexterm', 'flexterm-light']) {
    it(`${theme}: the keyboard focus ring shows at >= 3:1 where it is drawn`, () => {
      const tokens = themeTokens(theme)
      for (const bg of ['--bg-base', '--bg-elevated', '--bg-surface', '--bg-hover', '--bg-active']) {
        expect(contrastRatio(tokens['--focus-ring'], tokens[bg]), `${theme} --focus-ring on ${bg}`).toBeGreaterThanOrEqual(3)
      }
      for (const bg of ['--header-bg', '--header-tab-active']) {
        expect(contrastRatio(tokens['--header-focus-ring'], tokens[bg]), `${theme} --header-focus-ring on ${bg}`).toBeGreaterThanOrEqual(3)
      }
    })
  }

  it('header tabs paint text with solid tokens that the contrast test covers', () => {
    // The dropped (red) tab's shortcut hint uses the alert text color, and a
    // tab without a session gets a solid dimmer color, not opacity.
    expect(css).toMatch(/\.tab-item\.tab-down \.tab-shortcut \{\s*color: var\(--alert-text\);/)
    expect(css).toMatch(/\.tab-name\.tab-disconnected \{\s*opacity: 1;\s*color: var\(--header-text-3\);/)
  })

  it('the footer latency uses the colors the contrast test covers on the status bar', () => {
    const bar = Object.values(import.meta.glob('./components/TerminalStatusBar.vue', { query: '?raw', import: 'default', eager: true }))[0] as string
    expect(bar).toMatch(/\.terminal-status-bar \{[^}]*background: var\(--bg-elevated\);/)
    expect(bar).toMatch(/\.tsb-latency-off \{\s*color: var\(--text-muted\);/)
    expect(bar).toMatch(/\.tsb-latency-warn \{\s*color: var\(--warning\);/)
    expect(bar).toMatch(/\.tsb-latency-alert \{\s*color: var\(--error\);/)
  })

  it('the changed host key dialog paints its warning with a pair the contrast test covers', () => {
    const dlg = Object.values(import.meta.glob('./components/HostKeyChangedDialog.vue', { query: '?raw', import: 'default', eager: true }))[0] as string
    expect(dlg).toMatch(/\.hkc-alert \{[^}]*background: var\(--alert-card\);\s*color: var\(--alert-text\);/)
    expect(dlg).toMatch(/\.hkc-title \{[^}]*color: var\(--error\);/)
    // Replace is a red outline (text --error, covered on every surface);
    // the --danger-solid fill shows only on hover/press.
    expect(dlg).toMatch(/\.hkc-replace \{[^}]*--el-button-bg-color: transparent;[^}]*--el-button-text-color: var\(--error\);[^}]*--el-button-hover-bg-color: var\(--danger-solid\);[^}]*--el-button-hover-text-color: var\(--on-danger\);/)
    expect(dlg).toMatch(/\.hkc-downgrade \{[^}]*background: var\(--alert-card\);\s*color: var\(--alert-text\);/)
    expect(dlg).toMatch(/ref="safeRef" type="primary" class="hkc-cancel"/)
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

import { describe, expect, it, beforeEach, vi } from 'vitest'
import type { IBufferLine } from '@xterm/xterm'
import { matchTextSpans } from './highlightRules'
import {
  validateRule,
  compileUserRule,
  compileRules,
  type UserHighlightRule,
} from './userHighlightRules'
import {
  getLineHighlightColors,
  resolveTerminalRules,
  setHighlightRulesProvider,
} from './overlayHighlight'
import { migrateLegacy } from '../stores/highlightRuleStore'

// Settings store stand-in (the real one needs a browser `window`).
const settings = { terminal: { highlightEnabled: true } }
vi.mock('../stores/settingsStore', () => ({
  useSettingsStore: () => ({ settings }),
}))

const rule = (over: Partial<UserHighlightRule>): UserHighlightRule => ({
  id: 'r1',
  pattern: 'LOS',
  kind: 'keyword',
  color: '#ff0000',
  ...over,
})


function spansOf(text: string, r: UserHighlightRule) {
  const compiled = compileUserRule(r)
  expect(compiled).not.toBeNull()
  return matchTextSpans(text, [compiled!]).spans.map(s => text.slice(s.start, s.end))
}

describe('validateRule', () => {
  it('accepts plain keywords and sane regexes', () => {
    expect(validateRule(rule({})).ok).toBe(true)
    expect(validateRule(rule({ kind: 'regex', pattern: 'gpon-onu_\\d+/\\d+/\\d+:\\d+' })).ok).toBe(true)
    expect(validateRule(rule({ kind: 'regex', pattern: '-2[7-9]\\.\\d+' })).ok).toBe(true)
  })

  it.each([
    [{ pattern: '' }, 'empty'],
    [{ pattern: 'x'.repeat(2001) }, 'tooLong'],
    [{ color: 'red' }, 'badColor'],
    [{ color: '#fff' }, 'badColor'],
    [{ kind: 'regex' as const, pattern: '(' }, 'invalidRegex'],
    [{ kind: 'regex' as const, pattern: '(?<=a)b' }, 'lookbehind'],
    [{ kind: 'regex' as const, pattern: '(?<!a)b' }, 'lookbehind'],
    [{ kind: 'regex' as const, pattern: 'x*' }, 'matchesEmpty'],
    [{ kind: 'regex' as const, pattern: '^' }, 'matchesEmpty'],
  ])('rejects %j as %s', (over, code) => {
    expect(validateRule(rule(over)).code).toBe(code)
  })

  it.each(['(a+)+$', '(a|aa)+$', '(\\w+\\s?)+$', '(a*)*b'])(
    'rejects catastrophic backtracking %s quickly',
    (pattern) => {
      const t0 = performance.now()
      expect(validateRule(rule({ kind: 'regex', pattern })).code).toBe('tooSlow')
      expect(performance.now() - t0).toBeLessThan(1500)
    },
  )
})

describe('compileUserRule', () => {
  it('treats keywords literally', () => {
    expect(spansOf('rx: -24.5 (dbm) a.b', rule({ pattern: '(dbm)' }))).toEqual(['(dbm)'])
    expect(spansOf('axb a.b', rule({ pattern: 'a.b' }))).toEqual(['a.b'])
  })

  it('is case-insensitive by default and honors caseSensitive', () => {
    expect(spansOf('los LOS Los', rule({}))).toEqual(['los', 'LOS', 'Los'])
    expect(spansOf('los LOS Los', rule({ caseSensitive: true }))).toEqual(['LOS'])
  })

  it('wholeWord excludes glued matches and trims the left guard', () => {
    expect(spans2('CLOSED LOS xLOS LOS_1 (LOS)', rule({ wholeWord: true }))).toEqual(['LOS', 'LOS'])
  })

  it('does not trim user capture groups when wholeWord is off', () => {
    expect(spansOf('state: down', rule({ kind: 'regex', pattern: '(state): (down)' }))).toEqual(['state: down'])
  })

  it('skips disabled and invalid rules', () => {
    expect(compileUserRule(rule({ enabled: false }))).toBeNull()
    expect(compileUserRule(rule({ kind: 'regex', pattern: '(a+)+$' }))).toBeNull()
  })

  it('carries the explicit color onto spans', () => {
    const c = compileUserRule(rule({ color: '#00ff00' }))!
    expect(matchTextSpans('LOS', [c]).spans[0].color).toBe('#00ff00')
  })
})

function spans2(text: string, r: UserHighlightRule) {
  return spansOf(text, r)
}

describe('compileRules', () => {
  it('memoizes by content and keeps order', () => {
    const list = [rule({ id: 'a', pattern: 'error' }), rule({ id: 'b', pattern: 'DyingGasp' })]
    const a = compileRules(list)
    expect(compileRules(structuredClone(list))).toBe(a)
    expect(compileRules([list[1], list[0]])).not.toBe(a)
    expect(a).toHaveLength(2)
  })

  it('honors trimLead for rules with a guard group', () => {
    const r = compileUserRule(rule({ kind: 'regex', pattern: '(^|\\s)(/\\S+)', trimLead: true }))!
    const text = 'see /etc/hosts'
    const [span] = matchTextSpans(text, [r]).spans
    expect(text.slice(span.start, span.end)).toBe('/etc/hosts')
  })

  it('accepts theme colors', () => {
    expect(validateRule(rule({ color: 'theme:red' })).ok).toBe(true)
    expect(validateRule(rule({ color: 'theme:' })).code).toBe('badColor')
  })
})

describe('legacy migration', () => {
  it('puts old set rules first, disabling per-connection ones', () => {
    const out = migrateLegacy({
      version: 1,
      sets: [
        { name: 'Global', global: true, rules: [rule({ id: 'g', pattern: 'foo' })] },
        { name: 'OLT', global: false, rules: [rule({ id: 'o', pattern: 'bar' })] },
      ],
    })
    expect(out[0]).toMatchObject({ pattern: 'foo', group: 'Global' })
    expect(out[0].enabled).not.toBe(false)
    expect(out[1]).toMatchObject({ pattern: 'bar', group: 'OLT', enabled: false })
    expect(out.length).toBeGreaterThan(50) // defaults appended
  })
})

// Minimal IBufferLine stand-in: one char per cell, with per-column
// "remote already colored this" flags.
function fakeLine(text: string, coloredCols: number[] = []): IBufferLine {
  const colored = new Set(coloredCols)
  return {
    translateToString: () => text,
    getCell: (col: number) => ({
      getChars: () => text[col] ?? '',
      getWidth: () => 1,
      isFgDefault: () => !colored.has(col),
    }),
  } as unknown as IBufferLine
}

describe('terminal rule resolution', () => {
  beforeEach(() => {
    settings.terminal.highlightEnabled = true
    setHighlightRulesProvider(() => [])
  })

  it('returns the compiled list, or nothing when highlighting is off', () => {
    const compiled = compileRules([rule({})])
    setHighlightRulesProvider(() => compiled)
    expect(resolveTerminalRules()).toBe(compiled)
    settings.terminal.highlightEnabled = false
    expect(resolveTerminalRules()).toEqual([])
  })

  it('resolves theme colors from the terminal palette', () => {
    const compiled = compileRules([rule({ pattern: 'LOS', color: 'theme:red' })])
    setHighlightRulesProvider(() => compiled)
    expect(getLineHighlightColors(fakeLine('LOS'), 3, { red: '#aa0000' })[0]).toBe('#aa0000')
    expect(getLineHighlightColors(fakeLine('LOS'), 3, {})[0]).toBe('#cd3131') // xterm default
  })

  it('does not paint over text the remote side already colored', () => {
    const compiled = compileRules([rule({ pattern: 'LOS', color: '#ff0000' })])
    setHighlightRulesProvider(() => compiled)
    // "LOS" at cols 4-6; the remote colored col 5.
    const colors = getLineHighlightColors(fakeLine('onu LOS', [5]), 7, {})
    expect(colors.slice(4, 7)).toEqual(['#ff0000', undefined, '#ff0000'])
  })
})

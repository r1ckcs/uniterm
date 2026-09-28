import { describe, expect, it, beforeEach, vi } from 'vitest'
import type { IBufferLine } from '@xterm/xterm'
import { matchTextSpans } from './highlightRules'
import {
  validateRule,
  compileUserRule,
  compileRuleSets,
  activeRuleSets,
  type UserHighlightRule,
  type HighlightRuleSet,
} from './userHighlightRules'
import {
  getLineHighlightColors,
  resolveTerminalRules,
  setHighlightRuleSetsProvider,
} from './overlayHighlight'

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

const set = (id: string, global: boolean, rules: UserHighlightRule[]): HighlightRuleSet => ({
  id,
  name: id,
  global,
  rules,
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
    [{ pattern: 'x'.repeat(501) }, 'tooLong'],
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

describe('rule sets', () => {
  const g = set('global', true, [rule({ id: 'a', pattern: 'error' })])
  const olt = set('olt', false, [rule({ id: 'b', pattern: 'DyingGasp' })])

  it('applies global sets everywhere and session sets only when picked', () => {
    expect(activeRuleSets([g, olt]).map(s => s.id)).toEqual(['global'])
    expect(activeRuleSets([g, olt], ['olt']).map(s => s.id)).toEqual(['global', 'olt'])
  })

  it('memoizes compilation by content', () => {
    const a = compileRuleSets([g, olt])
    const b = compileRuleSets([structuredClone(g), structuredClone(olt)])
    expect(b).toBe(a)
    const c = compileRuleSets([g])
    expect(c).not.toBe(a)
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
    setHighlightRuleSetsProvider(() => [])
  })

  it('puts user rules before built-ins so they win overlaps', () => {
    setHighlightRuleSetsProvider(() => [set('g', true, [rule({ pattern: 'error', color: '#123456' })])])
    const colors = getLineHighlightColors(fakeLine('fatal error here'), 16, {})
    expect(colors[6]).toBe('#123456') // "error" → user color, not theme red
  })

  it('keeps user rules when built-in categories are switched off', () => {
    settings.terminal.highlightEnabled = false
    expect(resolveTerminalRules()).toEqual([])
    setHighlightRuleSetsProvider(() => [set('g', true, [rule({})])])
    expect(resolveTerminalRules()).toHaveLength(1)
  })

  it('only applies session sets to terminals that opted in', () => {
    settings.terminal.highlightEnabled = false
    setHighlightRuleSetsProvider(() => [set('olt', false, [rule({ pattern: 'LOS' })])])
    expect(resolveTerminalRules()).toHaveLength(0)
    expect(resolveTerminalRules(['olt'])).toHaveLength(1)
  })

  it('does not paint over text the remote side already colored', () => {
    setHighlightRuleSetsProvider(() => [set('g', true, [rule({ pattern: 'LOS', color: '#ff0000' })])])
    settings.terminal.highlightEnabled = false
    // "LOS" at cols 4-6; the remote colored col 5.
    const colors = getLineHighlightColors(fakeLine('onu LOS', [5]), 7, {})
    expect(colors.slice(4, 7)).toEqual(['#ff0000', undefined, '#ff0000'])
  })
})

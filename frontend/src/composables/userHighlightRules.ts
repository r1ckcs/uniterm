// User-defined keyword highlight rules: rule sets the user (or a bundled
// network preset) defines, compiled into the same HighlightRule shape the
// built-in categories use, but carrying an explicit color.
//
// A rule set applies to a terminal when it is enabled globally, or when the
// terminal's connection opts into it (per-session sets). User rules run
// before the built-in categories, so on overlap the user's color wins.
//
// User regexes run on the UI thread for every visible line, so they are
// validated before use: size cap, no lookbehind (unparseable on macOS <=12.3
// WebKit, like the built-ins), no empty matches, and a timing probe against
// adversarial input to reject catastrophic backtracking.
import type { HighlightRule } from './highlightRules'

export type UserRuleKind = 'keyword' | 'regex'

export interface UserHighlightRule {
  id: string
  pattern: string
  kind: UserRuleKind
  /** CSS hex color, #rrggbb. */
  color: string
  caseSensitive?: boolean
  /** Match only when not glued to letters/digits/underscore on either side. */
  wholeWord?: boolean
  /** Defaults to true. */
  enabled?: boolean
}

export interface HighlightRuleSet {
  id: string
  name: string
  /** Applies to every terminal when true; otherwise only to connections
   * that list this set's id. */
  global: boolean
  /** Bundled preset: read-only in the UI (duplicate to customize). */
  builtin?: boolean
  rules: UserHighlightRule[]
}

export const MAX_PATTERN_LENGTH = 500
/** A single rule exceeding this on the probe input is rejected as too slow. */
export const MAX_PROBE_MS = 8

export type RuleErrorCode =
  | 'empty'
  | 'tooLong'
  | 'badColor'
  | 'invalidRegex'
  | 'lookbehind'
  | 'matchesEmpty'
  | 'tooSlow'

export interface RuleValidation {
  ok: boolean
  code?: RuleErrorCode
  detail?: string
}

const COLOR_RE = /^#[0-9a-fA-F]{6}$/
const WORD_CHAR = 'A-Za-z0-9_'

function escapeRegExp(s: string): string {
  return s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

/** Regex source for a rule, before word guards. */
function ruleSource(rule: UserHighlightRule): string {
  return rule.kind === 'keyword' ? escapeRegExp(rule.pattern) : rule.pattern
}

// Timing probe. Catastrophic patterns ((a+)+$, (\w+\s?)+$, (.*a){n}) blow up
// exponentially or polynomially with input length, so each adversarial
// family is tried at growing lengths and stops at the first run over budget:
// the probe itself can never hang the UI on a length it has not yet shown
// to be safe (each step is at most ~16x the previous one).
const PROBE_LENGTHS = [8, 12, 16, 20, 24, 32, 48, 64, 96, 128, 200]
const PROBE_FAMILIES: Array<(n: number) => string> = [
  n => 'a'.repeat(n) + '!',
  n => 'a '.repeat(n >> 1) + '!',
  n => '1'.repeat(n) + 'x',
  n => ' '.repeat(n) + 'x',
  n => 'ab'.repeat(n >> 1) + '!',
  n => '-'.repeat(n) + ':',
]
// Realistic terminal rows, checked once each.
const PROBE_LINES: string[] = [
  'gpon-onu_1/2/3:45   enable   working   1(GPON)   -24.512(dbm)   ZTEG1A2B3C4D  2026-09-28 17:52:21',
  'Sep 28 17:52:21 core-01 %LINK-3-UPDOWN: Interface GigabitEthernet0/1, changed state to down',
  ' 0  R  ether1   ether   1500  1598  4064  48:A9:8A:12:34:56 ' + '='.repeat(80),
]

function probeTooSlow(re: RegExp, input: string): boolean {
  const t0 = performance.now()
  re.lastIndex = 0
  let guard = 0
  while (re.exec(input) !== null && guard++ < 200) {
    if (performance.now() - t0 > MAX_PROBE_MS) return true
  }
  return performance.now() - t0 > MAX_PROBE_MS
}

/** Validate a rule before it is saved or applied. */
export function validateRule(rule: UserHighlightRule): RuleValidation {
  if (!rule.pattern) return { ok: false, code: 'empty' }
  if (rule.pattern.length > MAX_PATTERN_LENGTH) return { ok: false, code: 'tooLong' }
  if (!COLOR_RE.test(rule.color)) return { ok: false, code: 'badColor' }
  if (rule.kind === 'regex' && /\(\?<[=!]/.test(rule.pattern)) {
    return { ok: false, code: 'lookbehind' }
  }
  let re: RegExp
  try {
    re = buildRegex(rule)
  } catch (e) {
    return { ok: false, code: 'invalidRegex', detail: e instanceof Error ? e.message : String(e) }
  }
  // Empty matches would highlight nothing and spin the matcher.
  re.lastIndex = 0
  const m = re.exec('')
  if (m && m[0].length === 0) return { ok: false, code: 'matchesEmpty' }
  const probe = new RegExp(re.source, re.flags.replace('g', ''))
  if (probe.test('') || probe.exec(' x ')?.[0] === '') {
    return { ok: false, code: 'matchesEmpty' }
  }
  for (const family of PROBE_FAMILIES) {
    for (const n of PROBE_LENGTHS) {
      if (probeTooSlow(re, family(n))) return { ok: false, code: 'tooSlow' }
    }
  }
  for (const line of PROBE_LINES) {
    if (probeTooSlow(re, line)) return { ok: false, code: 'tooSlow' }
  }
  return { ok: true }
}

/** Build the global regex for a rule. With wholeWord, group 1 is a consumed
 * left guard (trimmed by matchTextSpans, same convention as the built-in
 * rules); the right guard is a zero-width lookahead. Throws on bad regex. */
function buildRegex(rule: UserHighlightRule): RegExp {
  const flags = rule.caseSensitive ? 'g' : 'gi'
  const src = ruleSource(rule)
  if (rule.wholeWord) {
    return new RegExp(`(^|[^${WORD_CHAR}])(?:${src})(?![${WORD_CHAR}])`, flags)
  }
  return new RegExp(src, flags)
}

/** Compile one rule. Invalid or disabled rules yield null (skipped). */
export function compileUserRule(rule: UserHighlightRule): HighlightRule | null {
  if (rule.enabled === false) return null
  if (!validateRule(rule).ok) return null
  return {
    // Category is only consulted when no explicit color is set.
    category: 'info',
    color: rule.color,
    // Without the word guard there is no consumed group 1; the user's own
    // capture groups must not be mistaken for one.
    noLeadTrim: !rule.wholeWord,
    regexes: [buildRegex(rule)],
  }
}

/** Sets that apply to a terminal: global ones plus the connection's picks,
 * in the order the sets are defined. */
export function activeRuleSets(sets: HighlightRuleSet[], sessionSetIds: readonly string[] = []): HighlightRuleSet[] {
  const picked = new Set(sessionSetIds)
  return sets.filter(s => s.global || picked.has(s.id))
}

// Compilation (including the timing probe) is too costly to redo on every
// refresh; memoize by the sets' content.
const compileCache = new Map<string, HighlightRule[]>()
const COMPILE_CACHE_MAX = 32

/** Compile the rules of the active sets, memoized: the same content returns
 * the same array instance, so callers can detect rule changes by identity. */
export function compileRuleSets(sets: HighlightRuleSet[]): HighlightRule[] {
  const key = JSON.stringify(sets.map(s => s.rules))
  const hit = compileCache.get(key)
  if (hit) return hit
  const out: HighlightRule[] = []
  for (const set of sets) {
    for (const rule of set.rules) {
      const compiled = compileUserRule(rule)
      if (compiled) out.push(compiled)
    }
  }
  compileCache.set(key, out)
  if (compileCache.size > COMPILE_CACHE_MAX) {
    const oldest = compileCache.keys().next().value
    if (oldest !== undefined) compileCache.delete(oldest)
  }
  return out
}

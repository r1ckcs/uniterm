// Keyword highlight rules: one ordered list (highlightRules.json) that holds
// everything — the network rules and the former built-in categories alike —
// applied to every terminal. Earlier rules win overlaps.
//
// Colors are either fixed ("#rrggbb") or follow the terminal theme palette
// ("theme:red", "theme:brightBlue", …) so defaults stay legible on light and
// dark themes.
//
// User regexes run on the UI thread for every visible line, so they are
// validated before use: size cap, no lookbehind (unparseable on macOS <=12.3
// WebKit), no empty matches, and a timing probe against adversarial input to
// reject catastrophic backtracking.
import { SUPPORTS_MATCH_INDICES, type HighlightRule } from './highlightRules'

export type UserRuleKind = 'keyword' | 'regex'

export interface UserHighlightRule {
  id: string
  pattern: string
  kind: UserRuleKind
  /** "#rrggbb", or "theme:<palette key>" to follow the terminal theme. */
  color: string
  caseSensitive?: boolean
  /** Match only when not glued to letters/digits/underscore on either side. */
  wholeWord?: boolean
  /** Regex whose capture group 1 is a consumed left guard that must not be
   * colored (the former built-in rules use this convention). */
  trimLead?: boolean
  /** MobaXterm-style rule: the line is matched as marker+line+marker. */
  lineMarker?: string
  /** Color capture group 1 (MobaXterm "guard(word)guard" patterns). */
  colorGroup1?: boolean
  /** Free-text label used to organize the list (e.g. "OLT ZTE"). */
  group?: string
  /** Defaults to true. */
  enabled?: boolean
}

export const MAX_PATTERN_LENGTH = 2000
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

const COLOR_RE = /^(?:#[0-9a-fA-F]{6}|theme:[A-Za-z]{2,20})$/
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

// Validation (with its timing probe) is memoized: the editor re-validates the
// whole list on every keystroke.
const validationCache = new Map<string, RuleValidation>()

/** Validate a rule before it is saved or applied. */
export function validateRule(rule: UserHighlightRule): RuleValidation {
  const key = JSON.stringify([rule.pattern, rule.kind, rule.color, !!rule.caseSensitive, !!rule.wholeWord, rule.lineMarker ?? ''])
  const hit = validationCache.get(key)
  if (hit) return hit
  const result = validateRuleUncached(rule)
  if (validationCache.size > 2000) validationCache.clear()
  validationCache.set(key, result)
  return result
}

function validateRuleUncached(rule: UserHighlightRule): RuleValidation {
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
  // MobaXterm-style rules see the line wrapped in their marker.
  const w = (s: string) => (rule.lineMarker ? rule.lineMarker + s + rule.lineMarker : s)
  // Empty matches would highlight nothing and spin the matcher.
  const probe = new RegExp(re.source, re.flags.replace('g', ''))
  if (probe.exec(w(''))?.[0] === '' || probe.exec(w(' x '))?.[0] === '') {
    return { ok: false, code: 'matchesEmpty' }
  }
  for (const family of PROBE_FAMILIES) {
    for (const n of PROBE_LENGTHS) {
      if (probeTooSlow(re, w(family(n)))) return { ok: false, code: 'tooSlow' }
    }
  }
  for (const line of PROBE_LINES) {
    if (probeTooSlow(re, w(line))) return { ok: false, code: 'tooSlow' }
  }
  return { ok: true }
}

/** Build the global regex for a rule. With wholeWord, group 1 is a consumed
 * left guard (trimmed by matchTextSpans, same convention as the built-in
 * rules); the right guard is a zero-width lookahead. Throws on bad regex. */
function buildRegex(rule: UserHighlightRule): RegExp {
  // "d" (match indices) locates group 1 exactly for colorGroup1 rules.
  const flags = (rule.caseSensitive ? 'g' : 'gi') + (rule.colorGroup1 && SUPPORTS_MATCH_INDICES ? 'd' : '')
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
    // Group 1 is trimmed only when it is a guard we (wholeWord) or the rule
    // (trimLead) put there; a user's own capture groups are never trimmed.
    noLeadTrim: !(rule.wholeWord || rule.trimLead),
    lineMarker: rule.lineMarker || undefined,
    group1Only: !!rule.colorGroup1,
    regexes: [buildRegex(rule)],
  }
}

// Compilation (including the timing probe) is too costly to redo on every
// refresh; memoize by content.
const compileCache = new Map<string, HighlightRule[]>()
const COMPILE_CACHE_MAX = 16

/** Compile a rule list in order, skipping disabled/invalid rules. Memoized:
 * the same content returns the same array instance, so callers can detect
 * rule changes by identity. */
export function compileRules(rules: UserHighlightRule[]): HighlightRule[] {
  const key = JSON.stringify(rules)
  const hit = compileCache.get(key)
  if (hit) return hit
  const out: HighlightRule[] = []
  for (const rule of rules) {
    const compiled = compileUserRule(rule)
    if (compiled) out.push(compiled)
  }
  compileCache.set(key, out)
  if (compileCache.size > COMPILE_CACHE_MAX) {
    const oldest = compileCache.keys().next().value
    if (oldest !== undefined) compileCache.delete(oldest)
  }
  return out
}

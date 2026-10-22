// Brings default-list changes into a saved highlightRules.json without
// touching what the user made their own.
//
// The file remembers, per default rule id, a fingerprint of the rule as it
// was last handed to the user (the "baseline"). On load, against the
// current default list:
// - a default the user never touched (fingerprint still equals the
//   baseline) follows the code: updated if it changed, dropped if it was
//   removed from the code;
// - a default the user edited in any way (pattern, color, on/off, group…)
//   is kept as is;
// - a default the user deleted stays deleted (its id is in the baseline);
// - a default new to this file is inserted next to its neighbors in the
//   default list, since the first matching rule wins;
// - the user's own rules (ids that are not "d<number>") are never touched,
//   and the user's order is kept.
import type { UserHighlightRule } from './userHighlightRules'

/** Default rule ids are "d" + number; user rules get other ids. */
export function isDefaultId(id: string): boolean {
  return /^d\d+$/.test(id)
}

/** The rule's content in a canonical form (id excluded, omitted options
 * filled in), so equal rules fingerprint the same however they were saved. */
function canonical(r: UserHighlightRule): string {
  return JSON.stringify([
    r.kind,
    r.pattern,
    r.color,
    r.caseSensitive === true,
    r.wholeWord === true,
    r.trimLead === true,
    r.lineMarker ?? '',
    r.colorGroup1 === true,
    r.group ?? '',
    r.enabled !== false,
  ])
}

/** Short stable hash (cyrb53) of a rule's content, as hex. */
export function ruleFingerprint(r: UserHighlightRule): string {
  const s = canonical(r)
  let h1 = 0xdeadbeef
  let h2 = 0x41c6ce57
  for (let i = 0; i < s.length; i++) {
    const c = s.charCodeAt(i)
    h1 = Math.imul(h1 ^ c, 2654435761)
    h2 = Math.imul(h2 ^ c, 1597334677)
  }
  h1 = Math.imul(h1 ^ (h1 >>> 16), 2246822507) ^ Math.imul(h2 ^ (h2 >>> 13), 3266489909)
  h2 = Math.imul(h2 ^ (h2 >>> 16), 2246822507) ^ Math.imul(h1 ^ (h1 >>> 13), 3266489909)
  return (4294967296 * (2097151 & h2) + (h1 >>> 0)).toString(16)
}

/** Baseline for a list just taken from the defaults (seed, restore, merge). */
export function baselineOf(defaults: UserHighlightRule[]): Record<string, string> {
  const out: Record<string, string> = {}
  for (const d of defaults) out[d.id] = ruleFingerprint(d)
  return out
}

/** Signature of the whole default list (pinned by a test to DEFAULTS_VERSION). */
export function defaultsSignature(defaults: UserHighlightRule[]): string {
  return ruleFingerprint({ id: '', kind: 'keyword', color: '', pattern: defaults.map(d => `${d.id}=${ruleFingerprint(d)}`).join(',') })
}

export interface MergeResult {
  rules: UserHighlightRule[]
  baseline: Record<string, string>
  /** Defaults inserted into the list. */
  added: number
  /** Untouched defaults updated to the new code version. */
  updated: number
  /** Untouched defaults dropped because the code no longer has them. */
  removed: number
}

/**
 * Merge the current default list into the user's rules.
 *
 * `baseline` is the file's record of the defaults last handed out; undefined
 * for files written before it existed. For those, a default that still
 * matches the code is treated as untouched, one that differs as edited, and
 * one that is missing as new (it may come back once if the user had deleted
 * it). A new default whose pattern the user already has (same kind and
 * pattern, any id) is not inserted again.
 */
export function mergeDefaults(
  user: UserHighlightRule[],
  baseline: Record<string, string> | undefined,
  defaults: UserHighlightRule[],
): MergeResult {
  const byId = new Map(defaults.map(d => [d.id, d]))
  const base: Record<string, string> = baseline ? { ...baseline } : {}
  if (!baseline) {
    for (const u of user) {
      const d = byId.get(u.id)
      if (d) base[u.id] = ruleFingerprint(d)
    }
  }

  let updated = 0
  let removed = 0
  const rules: UserHighlightRule[] = []
  for (const u of user) {
    const known = isDefaultId(u.id) ? base[u.id] : undefined
    if (known === undefined || ruleFingerprint(u) !== known) {
      rules.push(u) // user's own, or a default the user edited
      continue
    }
    const d = byId.get(u.id)
    if (!d) {
      removed++
      continue
    }
    if (ruleFingerprint(d) !== known) updated++
    rules.push(d)
  }

  // New defaults go right after the nearest earlier default present in the
  // list (or before the nearest later one), keeping the default order.
  let added = 0
  const have = new Set(rules.flatMap(r => [r.id, `${r.kind}\n${r.pattern}`]))
  defaults.forEach((d, i) => {
    if (d.id in base || have.has(d.id) || have.has(`${d.kind}\n${d.pattern}`)) return
    const at = (id: string) => rules.findIndex(r => r.id === id)
    let pos = -1
    for (let j = i - 1; j >= 0 && pos < 0; j--) {
      const k = at(defaults[j].id)
      if (k >= 0) pos = k + 1
    }
    for (let j = i + 1; j < defaults.length && pos < 0; j++) pos = at(defaults[j].id)
    rules.splice(pos < 0 ? rules.length : pos, 0, d)
    have.add(d.id).add(`${d.kind}\n${d.pattern}`)
    added++
  })

  return { rules, baseline: baselineOf(defaults), added, updated, removed }
}

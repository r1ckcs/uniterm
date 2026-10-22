import { describe, expect, it } from 'vitest'
import { baselineOf, isDefaultId, mergeDefaults, ruleFingerprint } from './highlightMerge'
import type { UserHighlightRule } from './userHighlightRules'

const kw = (id: string, pattern: string, extra: Partial<UserHighlightRule> = {}): UserHighlightRule => ({
  id, pattern, kind: 'keyword', color: '#ff0000', group: 'G', enabled: true, ...extra,
})

// Defaults as they were when the user's file was last merged.
const V1 = [kw('d1', 'LOS'), kw('d2', 'down'), kw('d3', 'up'), kw('d4', 'offline')]
const ids = (rules: UserHighlightRule[]) => rules.map(r => r.id)

describe('isDefaultId', () => {
  it('only "d" + number', () => {
    expect(isDefaultId('d1')).toBe(true)
    expect(isDefaultId('d62')).toBe(true)
    expect(isDefaultId('hlr-abc')).toBe(false)
    expect(isDefaultId('dx1')).toBe(false)
    expect(isDefaultId('d')).toBe(false)
  })
})

describe('ruleFingerprint', () => {
  it('ignores the id and how omitted options were saved', () => {
    const a = kw('d1', 'LOS')
    expect(ruleFingerprint({ ...a, id: 'other' })).toBe(ruleFingerprint(a))
    expect(ruleFingerprint({ ...a, enabled: undefined, caseSensitive: false })).toBe(ruleFingerprint(a))
  })

  it('sees every user-visible edit', () => {
    const a = kw('d1', 'LOS')
    const fp = ruleFingerprint(a)
    for (const edit of [
      { pattern: 'LOSi' }, { color: '#00ff00' }, { enabled: false }, { kind: 'regex' as const },
      { caseSensitive: true }, { wholeWord: true }, { trimLead: true }, { colorGroup1: true },
      { lineMarker: '¨' }, { group: 'Mine' },
    ]) {
      expect(ruleFingerprint({ ...a, ...edit }), JSON.stringify(edit)).not.toBe(fp)
    }
  })
})

describe('mergeDefaults', () => {
  const base = baselineOf(V1)

  it('no code change → list untouched', () => {
    const user = [kw('hlr-1', 'mine'), ...V1]
    const r = mergeDefaults(user, base, V1)
    expect(r.rules).toEqual(user)
    expect([r.added, r.updated, r.removed]).toEqual([0, 0, 0])
    expect(r.baseline).toEqual(base)
  })

  it('new default goes after its predecessor in the default order', () => {
    const v2 = [V1[0], V1[1], kw('d5', 'LOSi'), V1[2], V1[3]]
    const user = [kw('hlr-1', 'mine'), V1[0], V1[1], kw('hlr-2', 'mine2'), V1[2], V1[3]]
    const r = mergeDefaults(user, base, v2)
    expect(ids(r.rules)).toEqual(['hlr-1', 'd1', 'd2', 'd5', 'hlr-2', 'd3', 'd4'])
    expect(r.added).toBe(1)
    expect(r.baseline).toEqual(baselineOf(v2))
  })

  it('new default at the head goes before the first default; consecutive new ones keep order', () => {
    const v2 = [kw('d5', 'a'), kw('d6', 'b'), ...V1, kw('d7', 'z')]
    const user = [kw('hlr-1', 'mine'), ...V1, kw('hlr-2', 'tail')]
    const r = mergeDefaults(user, base, v2)
    expect(ids(r.rules)).toEqual(['hlr-1', 'd5', 'd6', 'd1', 'd2', 'd3', 'd4', 'd7', 'hlr-2'])
    expect(r.added).toBe(3)
  })

  it('anchors on the user order when the user moved defaults around', () => {
    const v2 = [V1[0], kw('d5', 'new'), V1[1], V1[2], V1[3]]
    const user = [V1[3], V1[0], V1[2], V1[1]]
    expect(ids(mergeDefaults(user, base, v2).rules)).toEqual(['d4', 'd1', 'd5', 'd3', 'd2'])
  })

  it('appends when no default is left to anchor on', () => {
    const v2 = [...V1, kw('d5', 'new')]
    const user = [kw('hlr-1', 'mine')]
    // The user deleted every default: those stay deleted, the new one is appended.
    expect(ids(mergeDefaults(user, base, v2).rules)).toEqual(['hlr-1', 'd5'])
  })

  it('changed default the user never touched → updated in place', () => {
    const v2 = [V1[0], kw('d2', 'down', { color: '#123456' }), V1[2], V1[3]]
    const user = [V1[1], kw('hlr-1', 'mine'), V1[0], V1[2], V1[3]]
    const r = mergeDefaults(user, base, v2)
    expect(r.rules[0]).toEqual(v2[1])
    expect(ids(r.rules)).toEqual(['d2', 'hlr-1', 'd1', 'd3', 'd4'])
    expect(r.updated).toBe(1)
  })

  it('changed default the user edited (pattern, color, on/off) → user version kept', () => {
    const v2 = V1.map(d => ({ ...d, color: '#123456' }))
    const user = [
      { ...V1[0], pattern: 'LOS!' },
      { ...V1[1], color: '#00ff00' },
      { ...V1[2], enabled: false },
      V1[3],
    ]
    const r = mergeDefaults(user, base, v2)
    expect(r.rules.slice(0, 3)).toEqual(user.slice(0, 3))
    expect(r.rules[3]).toEqual(v2[3])
    expect(r.updated).toBe(1)
  })

  it('an edit survives later merges too', () => {
    const v2 = V1.map(d => ({ ...d, color: '#123456' }))
    const user = [{ ...V1[0], enabled: false }, V1[1], V1[2], V1[3]]
    const r1 = mergeDefaults(user, base, v2)
    const v3 = v2.map(d => ({ ...d, color: '#abcdef' }))
    const r2 = mergeDefaults(r1.rules, r1.baseline, v3)
    expect(r2.rules[0]).toEqual(user[0])
    expect(r2.rules.slice(1)).toEqual(v3.slice(1))
  })

  it('default removed from the code → dropped if untouched, kept if edited', () => {
    const v2 = [V1[0], V1[3]]
    const user = [V1[0], V1[1], { ...V1[2], color: '#00ff00' }, V1[3]]
    const r = mergeDefaults(user, base, v2)
    expect(ids(r.rules)).toEqual(['d1', 'd3', 'd4'])
    expect(r.removed).toBe(1)
    // The kept one is now the user's own: no longer tracked.
    expect(r.baseline).not.toHaveProperty('d3')
    const again = mergeDefaults(r.rules, r.baseline, v2)
    expect(ids(again.rules)).toEqual(['d1', 'd3', 'd4'])
  })

  it('default the user deleted stays deleted, even if the code changes it', () => {
    const v2 = [V1[0], kw('d2', 'down', { color: '#123456' }), V1[2], V1[3]]
    const user = [V1[0], V1[2], V1[3]]
    const r = mergeDefaults(user, base, v2)
    expect(ids(r.rules)).toEqual(['d1', 'd3', 'd4'])
    expect(r.added).toBe(0)
  })

  it('never touches the user\'s own rules, even one shaped like a default', () => {
    const mine = [kw('hlr-1', 'LOS', { color: '#010203' }), kw('hlr-2', 'x', { enabled: false })]
    const v2 = [kw('d1', 'LOS', { color: '#999999' }), V1[1]]
    const r = mergeDefaults([...mine, ...V1], base, v2)
    expect(r.rules.slice(0, 2)).toEqual(mine)
  })

  it('does not insert a new default whose pattern the user already has', () => {
    const v2 = [...V1, kw('d5', 'mine')]
    const r = mergeDefaults([kw('hlr-1', 'mine'), ...V1], base, v2)
    expect(ids(r.rules)).toEqual(['hlr-1', 'd1', 'd2', 'd3', 'd4'])
    expect(r.added).toBe(0)
    // …and does not offer it again later.
    expect(r.baseline).toHaveProperty('d5')
  })

  it('file without baseline: matching defaults follow the code, differing ones are kept, missing ones added', () => {
    const v2 = [V1[0], V1[1], V1[2], V1[3], kw('d5', 'new')]
    const user = [kw('hlr-1', 'mine'), V1[0], { ...V1[1], color: '#00ff00' }, V1[3]]
    const r = mergeDefaults(user, undefined, v2)
    expect(ids(r.rules)).toEqual(['hlr-1', 'd1', 'd2', 'd3', 'd4', 'd5'])
    expect(r.rules[2].color).toBe('#00ff00')
    expect(r.added).toBe(2)
    expect(r.baseline).toEqual(baselineOf(v2))
  })

  it('file without baseline whose defaults all match → nothing changes', () => {
    const r = mergeDefaults([...V1], undefined, V1)
    expect(r.rules).toEqual(V1)
    expect([r.added, r.updated, r.removed]).toEqual([0, 0, 0])
  })

  it('is idempotent', () => {
    const v2 = [kw('d5', 'a'), V1[0], kw('d2', 'down', { color: '#123456' }), V1[3]]
    const r1 = mergeDefaults([kw('hlr-1', 'mine'), ...V1], base, v2)
    const r2 = mergeDefaults(r1.rules, r1.baseline, v2)
    expect(r2.rules).toEqual(r1.rules)
    expect([r2.added, r2.updated, r2.removed]).toEqual([0, 0, 0])
  })
})

import { describe, it, expect, vi, beforeEach } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'

vi.mock('@wailsio/runtime', async (importOriginal) => ({
  ...(await importOriginal()),
  Events: { On: vi.fn(() => () => {}), Off: vi.fn() },
}))
vi.mock('../../bindings/github.com/ys-ll/uniterm/app', () => ({
  LoadHighlightRules: vi.fn(),
  SaveHighlightRules: vi.fn().mockResolvedValue(undefined),
}))
vi.mock('element-plus', () => ({ ElMessage: vi.fn() }))
vi.mock('../i18n', () => ({
  t: (key: string, params?: Record<string, unknown>) => (params ? `${key}:${JSON.stringify(params)}` : key),
}))

import { ElMessage } from 'element-plus'
import { LoadHighlightRules, SaveHighlightRules } from '../../bindings/github.com/ys-ll/uniterm/app'
import { adoptHighlightRules, defaultRules, useHighlightRuleStore, type HighlightRulesData } from './highlightRuleStore'
import { DEFAULTS_VERSION } from '../composables/highlightDefaults'
import { baselineOf } from '../composables/highlightMerge'
import type { UserHighlightRule } from '../composables/userHighlightRules'

const kw = (id: string, pattern: string, extra: Partial<UserHighlightRule> = {}): UserHighlightRule => ({
  id, pattern, kind: 'keyword', color: '#ff0000', group: 'G', enabled: true, ...extra,
})
const V1 = [kw('d1', 'LOS'), kw('d2', 'down')]
const V2 = [V1[0], kw('d3', 'LOSi'), kw('d2', 'down', { color: '#00ff00' })]
const ids = (rules: UserHighlightRule[]) => rules.map(r => r.id)

describe('adoptHighlightRules', () => {
  it('missing file → seeds the defaults with a baseline', () => {
    const r = adoptHighlightRules(null, V2)
    expect(r.rules).toEqual(V2)
    expect(r.baseline).toEqual(baselineOf(V2))
    expect(r.needsSave).toBe(true)
  })

  it('v1 file → its sets first, then the defaults', () => {
    const data: HighlightRulesData = {
      version: 1,
      sets: [
        { name: 'Mine', global: true, rules: [kw('a', 'x')] },
        { name: 'Conn', global: false, rules: [kw('b', 'y')] },
      ],
    }
    const r = adoptHighlightRules(data, V2)
    expect(ids(r.rules)).toEqual(['a', 'b', 'd1', 'd3', 'd2'])
    expect(r.rules[0].group).toBe('Mine')
    expect(r.rules[1].enabled).toBe(false)
    expect(r.baseline).toEqual(baselineOf(V2))
    expect(r.needsSave).toBe(true)
  })

  it('v2 file on an older default list → merged, own rules and edits kept', () => {
    const data: HighlightRulesData = {
      version: 2,
      rules: [kw('hlr-1', 'mine'), V1[0], { ...V1[1], enabled: false }],
      // Any version below the current one (0 means "no baseline").
      defaultsVersion: DEFAULTS_VERSION - 0.5,
      defaults: baselineOf(V1),
    }
    const r = adoptHighlightRules(data, V2)
    expect(ids(r.rules)).toEqual(['hlr-1', 'd1', 'd3', 'd2'])
    expect(r.rules[3].enabled).toBe(false) // user's off switch survives the color change
    expect(r.added).toBe(1)
    expect(r.needsSave).toBe(true)
  })

  it('v2 file without defaultsVersion (written before the merge) → merged without baseline', () => {
    const data: HighlightRulesData = { version: 2, rules: [kw('hlr-1', 'mine'), ...V1] }
    const r = adoptHighlightRules(data, V2)
    // d2 differs from the current code: treated as the user's edit and kept.
    expect(ids(r.rules)).toEqual(['hlr-1', 'd1', 'd3', 'd2'])
    expect(r.rules[3].color).toBe('#ff0000')
    expect(r.needsSave).toBe(true)
  })

  it('v2 file already on the current (or a newer) default list → untouched', () => {
    for (const v of [DEFAULTS_VERSION, DEFAULTS_VERSION + 1]) {
      const data: HighlightRulesData = { version: 2, rules: [V1[0]], defaultsVersion: v, defaults: { d1: 'x' } }
      const r = adoptHighlightRules(data, V2)
      expect(r.rules).toEqual([V1[0]])
      expect(r.baseline).toEqual({ d1: 'x' })
      expect(r.needsSave).toBe(false)
    }
  })
})

describe('useHighlightRuleStore', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.mocked(SaveHighlightRules).mockClear()
    vi.mocked(ElMessage).mockClear()
  })

  const saved = () => vi.mocked(SaveHighlightRules).mock.calls.at(-1)![0] as unknown as HighlightRulesData

  it('first run saves the defaults with defaultsVersion and the baseline', async () => {
    vi.mocked(LoadHighlightRules).mockResolvedValueOnce({ version: 0, rules: null } as any)
    const store = useHighlightRuleStore()
    await store.load()
    const data = saved()
    expect(data.version).toBe(2)
    expect(data.defaultsVersion).toBe(DEFAULTS_VERSION)
    expect(data.rules).toEqual(defaultRules())
    expect(data.defaults).toEqual(baselineOf(defaultRules()))
    expect(ElMessage).not.toHaveBeenCalled()
  })

  it('a file missing a current default gets it back in place, with a notice', async () => {
    const defaults = defaultRules()
    const mine = kw('hlr-1', 'mine')
    vi.mocked(LoadHighlightRules).mockResolvedValueOnce({
      version: 2,
      rules: [mine, ...defaults.filter(r => r.id !== 'd5')],
      defaultsVersion: DEFAULTS_VERSION - 1,
      defaults: baselineOf(defaults.filter(r => r.id !== 'd5')),
    } as any)
    const store = useHighlightRuleStore()
    await store.load()
    expect(store.rules).toEqual([mine, ...defaults])
    expect(saved().defaults).toEqual(baselineOf(defaults))
    expect(ElMessage).toHaveBeenCalledWith(expect.objectContaining({ message: 'hl.defaultsAdded:{"count":1}' }))
  })

  it('an up-to-date file is not rewritten', async () => {
    const defaults = defaultRules()
    vi.mocked(LoadHighlightRules).mockResolvedValueOnce({
      version: 2, rules: defaults, defaultsVersion: DEFAULTS_VERSION, defaults: baselineOf(defaults),
    } as any)
    await useHighlightRuleStore().load()
    expect(SaveHighlightRules).not.toHaveBeenCalled()
  })

  it('edits and "Restore defaults" keep saving the baseline', async () => {
    vi.mocked(LoadHighlightRules).mockResolvedValueOnce({ version: 0, rules: null } as any)
    const store = useHighlightRuleStore()
    await store.load()
    const baseline = saved().defaults

    await store.replace([kw('hlr-1', 'mine'), ...store.rules.slice(1)])
    expect(saved().defaults).toEqual(baseline)
    expect(saved().defaultsVersion).toBe(DEFAULTS_VERSION)

    await store.restoreDefaults()
    expect(saved().rules).toEqual(defaultRules())
    expect(saved().defaults).toEqual(baselineOf(defaultRules()))
  })
})

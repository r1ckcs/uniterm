import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { setLatencyApi, useLatencyStore, type LatencyApi } from './latencyStore'
import type { LatencySample } from '../utils/latency'

const flush = () => new Promise((r) => setTimeout(r, 0))

// No DOM in these tests: a window/document that only carry focus.
let hasFocus = true
const win = new EventTarget()
const doc = Object.assign(new EventTarget(), { hasFocus: () => hasFocus, visibilityState: 'visible' })
vi.stubGlobal('window', win)
vi.stubGlobal('document', doc)

function fakeApi() {
  let push: ((m: Record<string, LatencySample>) => void) | null = null
  const api: LatencyApi = {
    watch: vi.fn(async (ids: string[]) => Object.fromEntries(ids.map((id) => [id, { state: 'ok' as const, rttMs: 12, via: 'direct' as const }]))),
    onUpdate: (cb) => { push = cb; return () => {} },
  }
  return { api, push: (m: Record<string, LatencySample>) => push?.(m) }
}

describe('latency store', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    hasFocus = true
  })

  it('watches the visible sessions in one call per change and [] when none is left', async () => {
    const { api, push } = fakeApi()
    setLatencyApi(api)
    const st = useLatencyStore()
    st.hold('a'); st.hold('b'); st.hold('a')
    await flush()
    expect(api.watch).toHaveBeenCalledTimes(1)
    expect(api.watch).toHaveBeenLastCalledWith(['a', 'b'])
    expect(st.sampleOf('a')?.rttMs).toBe(12)

    push({ a: { state: 'ok', rttMs: 300, via: 'jump' }, b: { state: 'unavailable', rttMs: 0, reason: 'mosh' } })
    expect(st.sampleOf('a')?.via).toBe('jump')
    expect(st.sampleOf('b')?.reason).toBe('mosh')

    st.release('a') // still held by the split pane
    await flush()
    expect(api.watch).toHaveBeenCalledTimes(1)
    st.release('a'); st.release('b')
    await flush()
    expect(api.watch).toHaveBeenLastCalledWith([])
    expect(st.sampleOf('a')).toBeUndefined()
  })

  it('stops watching while the window is out of focus', async () => {
    const { api } = fakeApi()
    setLatencyApi(api)
    const st = useLatencyStore()
    st.hold('a')
    await flush()
    hasFocus = false
    win.dispatchEvent(new Event('blur'))
    await flush()
    expect(api.watch).toHaveBeenLastCalledWith([])
    hasFocus = true
    win.dispatchEvent(new Event('focus'))
    await flush()
    expect(api.watch).toHaveBeenLastCalledWith(['a'])
  })

  it('without the backend the footer shows no latency', async () => {
    setLatencyApi(null)
    const st = useLatencyStore()
    st.hold('a')
    await flush()
    expect(st.sampleOf('a')).toBeUndefined()
  })
})

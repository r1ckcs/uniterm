// Latency of the visible terminal sessions (CONTRATO-latencia v1). Each
// visible footer holds its session id; the store passes the set to
// SessionLatencyWatch while the window has focus, and [] otherwise, so the
// backend only samples what is on screen.
//
// Like connStatusStore, it detects the binding: without it the footer shows
// no latency. A mock for development and screenshots: ?latency=demo (or
// localStorage['uniterm.latencyMock'] = 'demo').
import { defineStore } from 'pinia'
import { ref } from 'vue'
import * as App from '../../bindings/github.com/ys-ll/uniterm/app'
import { Events } from '@wailsio/runtime'
import type { LatencySample } from '../utils/latency'

export interface LatencyApi {
  watch(ids: string[]): Promise<Record<string, LatencySample>>
  onUpdate(cb: (m: Record<string, LatencySample>) => void): () => void
}

type Binding = (...args: unknown[]) => Promise<unknown>
const B = App as unknown as Record<string, Binding | undefined>

function realApi(): LatencyApi | null {
  const watch = B.SessionLatencyWatch
  if (typeof watch !== 'function') return null
  return {
    watch: (ids) => watch(ids) as Promise<Record<string, LatencySample>>,
    onUpdate: (cb) => Events.On('session:latency', (ev: { data: Record<string, LatencySample> }) => cb(ev.data)),
  }
}

// Demo: the n-th watched id gets 0, 23, 180 or 520 ms, cycling the routes.
export function createMockLatencyApi(): LatencyApi {
  const rtts = [0, 23, 180, 520]
  const vias: LatencySample['via'][] = ['direct', 'direct', 'jump', 'proxy']
  return {
    watch: async (ids) => Object.fromEntries(ids.map((id, i) => [id, {
      state: 'ok' as const, rttMs: rtts[i % 4], via: vias[i % 4], at: new Date().toISOString(),
    }])),
    onUpdate: () => () => {},
  }
}

function mockRequested(): boolean {
  try {
    const q = new URLSearchParams(window.location.search).get('latency')
    return (q ?? localStorage.getItem('uniterm.latencyMock')) === 'demo'
  } catch {
    return false
  }
}

let api: LatencyApi | null | undefined

export function setLatencyApi(a: LatencyApi | null | undefined) {
  api = a
}

const focused = () => document.hasFocus() && document.visibilityState === 'visible'

export const useLatencyStore = defineStore('latency', () => {
  const samples = ref<Record<string, LatencySample>>({})
  // Session id → number of visible footers showing it (split panes, a tab
  // shown in a workspace too).
  const holders = new Map<string, number>()
  let started = false
  let lastSent = ''
  let pending = false

  function resolveApi(): LatencyApi | null {
    if (api === undefined) api = realApi() ?? (mockRequested() ? createMockLatencyApi() : null)
    return api
  }

  function start() {
    if (started) return
    started = true
    const a = resolveApi()
    if (!a) return
    a.onUpdate((m) => { samples.value = { ...samples.value, ...(m || {}) } })
    window.addEventListener('focus', schedule)
    window.addEventListener('blur', schedule)
    document.addEventListener('visibilitychange', schedule)
  }

  // Tab switches deactivate one footer and activate another in the same
  // tick: batch them into one call.
  function schedule() {
    if (pending) return
    pending = true
    queueMicrotask(sync)
  }

  async function sync() {
    pending = false
    const a = api
    if (!a) return
    const ids = focused() ? [...holders.keys()].sort() : []
    const key = ids.join('\n')
    if (key === lastSent) return
    lastSent = key
    try {
      const m = await a.watch(ids)
      if (ids.length) samples.value = { ...samples.value, ...(m || {}) }
    } catch {
      // backend not ready: the footer just shows no latency
    }
  }

  function hold(id: string) {
    if (!id) return
    start()
    holders.set(id, (holders.get(id) || 0) + 1)
    schedule()
  }

  function release(id: string) {
    const n = holders.get(id)
    if (!n) return
    if (n > 1) holders.set(id, n - 1)
    else {
      holders.delete(id)
      const { [id]: _gone, ...rest } = samples.value
      samples.value = rest
    }
    schedule()
  }

  const sampleOf = (id: string | null | undefined): LatencySample | undefined => (id ? samples.value[id] : undefined)

  return { samples, hold, release, sampleOf }
})

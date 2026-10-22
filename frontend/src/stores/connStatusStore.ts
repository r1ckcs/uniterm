// Reachability status and recent-access times for the start screen
// (CONTRATO-status v1). The backend probes only while a start screen is
// visible and the window has focus; this store counts the screens that ask
// for it and drives ConnStatusSetActive.
//
// Like the vault screens, the store detects the bindings: until the backend
// lands the map stays empty and the start screen shows plain counts. A mock
// for development and screenshots: ?connstatus=demo (or
// localStorage['uniterm.connStatusMock'] = 'demo').
import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import * as App from '../../bindings/github.com/ys-ll/uniterm/app'
import { Events } from '@wailsio/runtime'

export type ConnState = 'ok' | 'down' | 'jump' | 'unchecked'

export interface ConnStatus {
  state: ConnState
  latencyMs?: number
  since?: string
  checkedAt?: string
  reason?: 'disabled' | 'pending' | 'type' | 'host' | 'limit'
}

export interface RecentAccess {
  id: string
  at: string
}

interface ConnStatusApi {
  get(): Promise<Record<string, ConnStatus>>
  setActive(active: boolean): Promise<void>
  checkNow(): Promise<void>
  onChanged(cb: (m: Record<string, ConnStatus>) => void): () => void
  recent?(): Promise<RecentAccess[]>
  onRecent?(cb: (r: RecentAccess[]) => void): () => void
}

type Binding = (...args: unknown[]) => Promise<unknown>
const B = App as unknown as Record<string, Binding | undefined>

function realApi(): ConnStatusApi | null {
  if (typeof B.ConnStatusGet !== 'function') return null
  const call = <T>(name: string, ...args: unknown[]) => (B[name] as Binding)(...args) as Promise<T>
  const on = <T>(name: string, cb: (v: T) => void) => Events.On(name, (ev: { data: T }) => cb(ev.data))
  return {
    get: () => call('ConnStatusGet'),
    setActive: (a) => call('ConnStatusSetActive', a),
    checkNow: () => call('ConnStatusCheckNow'),
    onChanged: (cb) => on('connstatus:changed', cb),
    ...(typeof B.GetRecentAccess === 'function'
      ? {
          recent: () => call<RecentAccess[]>('GetRecentAccess'),
          onRecent: (cb: (r: RecentAccess[]) => void) => on('store:recent:changed', cb),
        }
      : {}),
  }
}

// Demo: every 4th connection is down, every 7th goes through a jump host.
export function createMockConnStatusApi(ids: () => string[]): ConnStatusApi {
  const build = () => {
    const m: Record<string, ConnStatus> = {}
    const now = new Date().toISOString()
    ids().forEach((id, i) => {
      if (i % 4 === 3) m[id] = { state: 'down', since: new Date(Date.now() - 6 * 60_000).toISOString(), checkedAt: now }
      else if (i % 7 === 6) m[id] = { state: 'jump' }
      else m[id] = { state: 'ok', latencyMs: 4 + i, checkedAt: now }
    })
    return m
  }
  const listeners = new Set<(m: Record<string, ConnStatus>) => void>()
  return {
    get: async () => build(),
    setActive: async () => {},
    checkNow: async () => { const m = build(); listeners.forEach((l) => l(m)) },
    onChanged: (cb) => { listeners.add(cb); return () => listeners.delete(cb) },
  }
}

function mockRequested(): boolean {
  try {
    const q = new URLSearchParams(window.location.search).get('connstatus')
    return (q ?? localStorage.getItem('uniterm.connStatusMock')) === 'demo'
  } catch {
    return false
  }
}

let api: ConnStatusApi | null | undefined

export function setConnStatusApi(a: ConnStatusApi | null | undefined) {
  api = a
}

export const useConnStatusStore = defineStore('connStatus', () => {
  const map = ref<Record<string, ConnStatus>>({})
  const recent = ref<RecentAccess[] | null>(null)
  // True once the backend (or the mock) answered: without it the start
  // screen falls back to plain counts.
  const available = ref(false)
  let started = false
  let activeCount = 0

  function resolveApi(idsForMock: () => string[]): ConnStatusApi | null {
    if (api === undefined) api = realApi() ?? (mockRequested() ? createMockConnStatusApi(idsForMock) : null)
    return api
  }

  async function start(idsForMock: () => string[] = () => []) {
    if (started) return
    started = true
    const a = resolveApi(idsForMock)
    if (!a) return
    a.onChanged((m) => { map.value = m || {}; available.value = true })
    try {
      map.value = (await a.get()) || {}
      available.value = true
    } catch {
      // backend not ready: keep the plain counts
    }
    if (a.recent) {
      a.onRecent?.((r) => { recent.value = r || [] })
      try { recent.value = (await a.recent()) || [] } catch { recent.value = null }
    }
  }

  // Each visible+focused start screen calls acquire(); the backend probes
  // while at least one holds it.
  function acquire() {
    activeCount++
    if (activeCount === 1) api?.setActive(true).catch(() => {})
  }
  function release() {
    if (activeCount === 0) return
    activeCount--
    if (activeCount === 0) api?.setActive(false).catch(() => {})
  }

  function checkNow() {
    api?.checkNow().catch(() => {})
  }

  const stateOf = (id: string): ConnState | undefined => map.value[id]?.state
  const statusOf = (id: string): ConnStatus | undefined => map.value[id]
  const downCount = computed(() => Object.values(map.value).filter((s) => s.state === 'down').length)

  return { map, recent, available, start, acquire, release, checkNow, stateOf, statusOf, downCount }
})

// Credential vault API seen by the UI (CONTRATO-cofre-f1.md §1, §5).
//
// `realVaultApi()` adapts the Wails bindings; when they are not in the build
// yet (or the backend is unreachable in browser debug mode) the mock below
// drives the whole flow instead.
//
// Mock selection, for development and screenshots only:
//   ?vault=<scenario> in the URL, or localStorage 'uniterm.vaultMock'
//   scenarios: welcome | ready | join | newDevice | recovery | revoked | error | test
import * as App from '../../bindings/github.com/ys-ll/uniterm/app'
import { Events } from '@wailsio/runtime'
import { useConnectionStore } from '../stores/connectionStore'
import type {
  VaultChange, VaultDevice, VaultJoinRequest, VaultRevokeResult, VaultRole, VaultStatus, VaultSyncResult,
} from '../types/vault'

export interface VaultApi {
  getStatus(): Promise<VaultStatus>
  signIn(): Promise<void>
  cancelSignIn(): Promise<void>
  skip(): Promise<void>
  recoveryWords(): Promise<string[]>
  confirmRecovery(positions: number[], words: string[]): Promise<void>
  recoverWithWords(words: string[]): Promise<void>
  listDevices(): Promise<VaultDevice[]>
  revokeDevice(id: string): Promise<VaultRevokeResult>
  renameThisDevice(name: string): Promise<void>
  listJoinRequests(): Promise<VaultJoinRequest[]>
  // Step 1 on the approver: the new device only shows its code after this.
  beginApproval(requestId: string): Promise<void>
  // Step 2, once the request comes back with ready: true. `code` is what the user typed from the NEW device's screen.
  // Resolves to the 4-digit confirmation the user types on the new device.
  approveJoin(requestId: string, code: string, role: VaultRole): Promise<string>
  // New device: the 4 digits shown on the approver.
  completeJoin(code4: string): Promise<void>
  // New device: give up the request (stays signed in, phase → off).
  cancelJoin(): Promise<void>
  rejectJoin(requestId: string): Promise<void>
  syncNow(): Promise<VaultSyncResult>
  // New recovery key: nothing is written; phase → showRecovery with
  // recoveryRegen, then the same 24-word flow as the first device (CONTRATO v6).
  // Optional so a build without the binding shows only the warning.
  regenerateRecovery?: () => Promise<void>
  // Gives up a regeneration: back to ready, the old phrase stays valid.
  cancelRecovery(): Promise<void>
  signOut(): Promise<void>
  changedMap(): Promise<Record<string, VaultChange>>
  onStatus(cb: (s: VaultStatus) => void): () => void
  onJoinRequest(cb: (r: VaultJoinRequest) => void): () => void
  onChanged(cb: (m: Record<string, VaultChange>) => void): () => void
}

// Rejections carry "vault.xxx.yyy: detail"; only the code reaches the screen.
export class VaultError extends Error {
  constructor(public code: string) {
    super(code)
  }
}

export function vaultErrorCode(e: unknown): string {
  if (e instanceof VaultError) return e.code
  const m = String((e as { message?: string })?.message ?? e)
  const hit = m.match(/^(vault\.[A-Za-z.]+)/)
  return hit ? hit[1] : 'vault.internal'
}

type Binding = (...args: unknown[]) => Promise<unknown>
const B = App as unknown as Record<string, Binding | undefined>

function realVaultApi(): VaultApi | null {
  if (typeof B.VaultGetStatus !== 'function') return null
  const call = <T>(name: string, ...args: unknown[]) => (B[name] as Binding)(...args) as Promise<T>
  const on = <T>(name: string, cb: (v: T) => void) => Events.On(name, (ev: { data: T }) => cb(ev.data))
  return {
    getStatus: () => call('VaultGetStatus'),
    signIn: () => call('VaultSignIn'),
    cancelSignIn: () => call('VaultCancelSignIn'),
    skip: () => call('VaultSkip'),
    recoveryWords: () => call('VaultRecoveryWords'),
    confirmRecovery: (idx, words) => call('VaultConfirmRecovery', idx, words),
    recoverWithWords: (words) => call('VaultRecoverWithWords', words),
    listDevices: async () => (await call<VaultDevice[] | null>('VaultListDevices')) ?? [],
    revokeDevice: (id) => call('VaultRevokeDevice', id),
    renameThisDevice: (name) => call('VaultRenameThisDevice', name),
    listJoinRequests: async () => (await call<VaultJoinRequest[] | null>('VaultListJoinRequests')) ?? [],
    beginApproval: (id) => call('VaultBeginApproval', id),
    approveJoin: (id, code, role) => call('VaultApproveJoin', id, code, role),
    rejectJoin: (id) => call('VaultRejectJoin', id),
    completeJoin: (code4) => call('VaultCompleteJoin', code4),
    cancelJoin: () => call('VaultCancelJoin'),
    syncNow: () => call('VaultSyncNow'),
    regenerateRecovery: typeof B.VaultRegenerateRecovery === 'function' ? () => call('VaultRegenerateRecovery') : undefined,
    cancelRecovery: () => call('VaultCancelRecovery'),
    signOut: () => call('VaultSignOut'),
    changedMap: async () => (await call<Record<string, VaultChange> | null>('VaultChangedMap')) ?? {},
    onStatus: (cb) => on('vault:status', cb),
    onJoinRequest: (cb) => on('vault:joinRequest', cb),
    onChanged: (cb) => on('vault:changed', (m: Record<string, VaultChange> | null) => cb(m ?? {})),
  }
}

function mockScenario(): string | null {
  try {
    const q = new URLSearchParams(window.location.search).get('vault')
    if (q) return q
    return window.localStorage.getItem('uniterm.vaultMock')
  } catch {
    return null
  }
}

let api: VaultApi | null = null

export function getVaultApi(): VaultApi {
  if (api) return api
  const scenario = mockScenario()
  api = (scenario ? null : realVaultApi()) ?? createMockVaultApi(scenario ?? 'idle', 1500, () =>
    useConnectionStore().connections.map((c) => c.id))
  return api
}

// Test hook.
export function setVaultApi(next: VaultApi | null) {
  api = next
}

// ── Mock ─────────────────────────────────────────────────────────────────

const MOCK_WORDS = [
  'abandon', 'harbor', 'velvet', 'oxygen', 'timber', 'canyon', 'lunar', 'pistol',
  'dragon', 'museum', 'orbit', 'safari', 'echo', 'garden', 'quote', 'rival',
  'silent', 'tunnel', 'walnut', 'bridge', 'cactus', 'fringe', 'motion', 'zebra',
]
const MOCK_CODE = '482915'
const MOCK_CONFIRM = '7351'
const MOCK_CHECK = [3, 10, 18]

const hoursAgo = (h: number) => new Date(Date.now() - h * 3_600_000).toISOString()
const minutesAhead = (m: number) => new Date(Date.now() + m * 60_000).toISOString()

export function createMockVaultApi(scenario: string, delayMs = 1500, connectionIds: () => string[] = () => []): VaultApi {
  const email = scenario === 'test' ? 'file:/tmp/cofre' : 'henrique@example.com'
  const status: VaultStatus = {
    phase: 'off',
    promptWelcome: scenario === 'welcome',
    testMode: scenario === 'test',
    device: { id: 'dev-mac-escritorio', name: 'MacBook do escritório', fingerprint: '7F3A 91C2 0B5E 44D8', role: 'admin' },
    syncing: false,
    offline: false,
    pendingOps: 0,
  }
  const me = (): VaultDevice => ({ ...status.device, role: 'admin', isThis: true, isRecovery: false, lastSeenAt: new Date().toISOString() })
  let others: VaultDevice[] = []
  const signedIn = (patch: Partial<VaultStatus>) => Object.assign(status, { account: { email } }, patch)

  if (['ready', 'join', 'test'].includes(scenario)) {
    signedIn({ phase: 'ready', lastSyncAt: hoursAgo(0.05) })
    others = [
      { id: 'dev-mac-casa', name: 'MacBook de casa', fingerprint: 'C41E 08AF 6D2B 9930', role: 'admin', isThis: false, isRecovery: false, lastSeenAt: hoursAgo(20) },
      { id: 'dev-mac-noc', name: 'Mac mini do NOC', fingerprint: '2B90 E7D1 55A3 0C6F', role: 'admin', isThis: false, isRecovery: false, lastSeenAt: hoursAgo(24 * 9) },
      { id: 'recovery', name: 'recovery', fingerprint: '91AA 3E07 C5D2 18F4', role: 'admin', isThis: false, isRecovery: true },
    ]
  }
  if (scenario === 'test') Object.assign(status, { offline: true, pendingOps: 3 })
  if (scenario === 'newDevice') {
    signedIn({ phase: 'awaitingApproval', pendingJoin: { fingerprint: status.device.fingerprint, expiresAt: minutesAhead(10), approved: false, attemptsLeft: 3 } })
    // An existing device starts the approval a moment later (the code appears),
    // then accepts it (this device asks for the 4 digits).
    setTimeout(() => {
      if (status.phase === 'awaitingApproval') set({ pendingJoin: { ...status.pendingJoin!, code6: MOCK_CODE } })
    }, delayMs * 1.5)
    setTimeout(() => {
      if (status.phase === 'awaitingApproval') set({ pendingJoin: { ...status.pendingJoin!, approved: true } })
    }, delayMs * 4)
  }
  if (scenario === 'recovery') signedIn({ phase: 'showRecovery', recoveryCheck: MOCK_CHECK })
  if (scenario === 'revoked') signedIn({ phase: 'revoked' })
  if (scenario === 'error') signedIn({ phase: 'error', error: { code: 'vault.integrity', message: 'roster rollback' } })

  const statusCbs = new Set<(s: VaultStatus) => void>()
  const joinCbs = new Set<(r: VaultJoinRequest) => void>()
  let joinRequests: VaultJoinRequest[] = []
  let timer: ReturnType<typeof setTimeout> | undefined
  const emit = () => statusCbs.forEach((cb) => cb(structuredClone(status)))
  const set = (patch: Partial<VaultStatus>) => {
    Object.assign(status, patch)
    emit()
  }
  const later = (fn: () => void, ms = delayMs) => {
    clearTimeout(timer)
    timer = setTimeout(fn, ms)
  }

  if (scenario === 'join') {
    setTimeout(() => {
      const r = { requestId: 'req-1', name: 'MacBook Air da sala', fingerprint: '5D07 A1F9 3C8E 6B24', expiresAt: minutesAhead(10), ready: false, attemptsLeft: 3 }
      joinRequests = [r]
      joinCbs.forEach((cb) => cb(r))
    }, 800)
  }

  return {
    async getStatus() { return structuredClone(status) },
    async signIn() {
      set({ phase: 'signingIn', error: undefined })
      later(() => {
        signedIn({ phase: 'creating' })
        emit()
        later(() => set({ phase: 'showRecovery', recoveryCheck: MOCK_CHECK }), 400)
      })
    },
    async cancelSignIn() {
      clearTimeout(timer)
      set({ phase: 'off', account: undefined, error: { code: 'vault.oauth.cancelled', message: '' } })
    },
    async skip() { set({ promptWelcome: false }) },
    async recoveryWords() {
      if (status.phase !== 'showRecovery') throw new VaultError('vault.state.wrongPhase')
      return [...MOCK_WORDS]
    },
    async confirmRecovery(positions, words) {
      const ok = positions.every((p, i) => MOCK_WORDS[p] === words[i]?.trim().toLowerCase())
      if (!ok) throw new VaultError('vault.recovery.mismatch')
      set({ phase: 'ready', recoveryCheck: undefined, recoveryRegen: false, lastSyncAt: new Date().toISOString() })
    },
    async recoverWithWords(words) {
      if (words.length !== MOCK_WORDS.length) throw new VaultError('vault.recovery.badWords')
      if (words.join(' ') !== MOCK_WORDS.join(' ')) throw new VaultError('vault.recovery.notThisVault')
      set({ phase: 'ready', pendingJoin: undefined, lastSyncAt: new Date().toISOString() })
    },
    async listDevices() { return [me(), ...others.map((d) => ({ ...d }))] },
    async revokeDevice(id) {
      if (id === status.device.id) throw new VaultError('vault.revoke.self')
      const wasAdmin = others.find((d) => d.id === id)?.role === 'admin'
      others = others.filter((d) => d.id !== id)
      const conns = useConnectionStore().connections as { id: string; name?: string; host?: string }[]
      return {
        exposed: conns.slice(0, 3).map((c) => ({ connectionId: c.id, name: c.name ?? c.id, host: c.host ?? '' })),
        recoveryStale: wasAdmin,
      }
    },
    async renameThisDevice(name) {
      status.device = { ...status.device, name }
      emit()
    },
    async listJoinRequests() { return [...joinRequests] },
    async beginApproval(requestId) {
      const r = joinRequests.find((x) => x.requestId === requestId)
      if (!r) throw new VaultError('vault.join.notFound')
      setTimeout(() => {
        const next = { ...r, ready: true }
        joinRequests = joinRequests.map((x) => (x.requestId === requestId ? next : x))
        joinCbs.forEach((cb) => cb(next))
      }, delayMs)
    },
    async approveJoin(requestId, code) {
      const r = joinRequests.find((x) => x.requestId === requestId)
      if (!r) throw new VaultError('vault.join.notFound')
      if (!r.ready) throw new VaultError('vault.join.notReady')
      if (code !== MOCK_CODE) {
        const next = { ...r, attemptsLeft: r.attemptsLeft - 1 }
        if (next.attemptsLeft <= 0) {
          joinRequests = joinRequests.filter((x) => x !== r)
          throw new VaultError('vault.join.tooManyAttempts')
        }
        joinRequests = joinRequests.map((x) => (x === r ? next : x))
        joinCbs.forEach((cb) => cb(next))
        throw new VaultError('vault.join.badCode')
      }
      joinRequests = joinRequests.filter((x) => x !== r)
      others.push({ id: 'dev-air-sala', name: r.name, fingerprint: r.fingerprint, role: 'admin', isThis: false, isRecovery: false, lastSeenAt: new Date().toISOString() })
      return MOCK_CONFIRM
    },
    async completeJoin(code4) {
      const pj = status.pendingJoin
      if (status.phase !== 'awaitingApproval' || !pj?.approved) throw new VaultError('vault.state.wrongPhase')
      if (code4 !== MOCK_CONFIRM) {
        if (pj.attemptsLeft <= 1) {
          set({ phase: 'off', pendingJoin: undefined })
          throw new VaultError('vault.join.tooManyAttempts')
        }
        set({ pendingJoin: { ...pj, attemptsLeft: pj.attemptsLeft - 1 } })
        throw new VaultError('vault.join.badCode')
      }
      set({ phase: 'ready', pendingJoin: undefined, lastSyncAt: new Date().toISOString() })
    },
    async cancelJoin() {
      clearTimeout(timer)
      set({ phase: 'off', pendingJoin: undefined })
    },
    async rejectJoin(requestId) { joinRequests = joinRequests.filter((x) => x.requestId !== requestId) },
    async syncNow() {
      set({ syncing: true })
      await new Promise((r) => setTimeout(r, Math.min(delayMs, 900)))
      set({ syncing: false, lastSyncAt: new Date().toISOString(), pendingOps: 0, offline: false })
      return { pulled: 2, pushed: 0, rejected: 0, warnings: [] }
    },
    async regenerateRecovery() {
      if (status.phase !== 'ready') throw new VaultError('vault.state.wrongPhase')
      set({ phase: 'showRecovery', recoveryCheck: MOCK_CHECK, recoveryRegen: true })
    },
    async cancelRecovery() {
      if (!status.recoveryRegen) throw new VaultError('vault.state.wrongPhase')
      set({ phase: 'ready', recoveryCheck: undefined, recoveryRegen: false })
    },
    async signOut() {
      clearTimeout(timer)
      others = []
      set({ phase: 'off', account: undefined, lastSyncAt: undefined, pendingJoin: undefined })
    },
    async changedMap() {
      if (status.phase !== 'ready') return {}
      // Pretend every third connection was last edited on the home Mac.
      const out: Record<string, VaultChange> = {}
      connectionIds().forEach((id, i) => {
        if (i % 3 === 1) out[id] = { deviceId: 'dev-mac-casa', deviceName: 'MacBook de casa', changedAt: hoursAgo(20 + i), fields: i === 1 ? ['host', 'port'] : ['password'] }
      })
      return out
    },
    onStatus(cb) { statusCbs.add(cb); return () => statusCbs.delete(cb) },
    onJoinRequest(cb) { joinCbs.add(cb); return () => joinCbs.delete(cb) },
    onChanged() { return () => {} },
  }
}

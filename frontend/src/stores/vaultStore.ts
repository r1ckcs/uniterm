import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { getVaultApi, vaultErrorCode } from '../services/vaultApi'
import type { VaultChange, VaultDevice, VaultJoinRequest, VaultRevokeResult, VaultRole, VaultStatus } from '../types/vault'

const EMPTY: VaultStatus = {
  phase: 'off',
  promptWelcome: false,
  testMode: false,
  device: { id: '', name: '', fingerprint: '' },
  syncing: false,
  offline: false,
  pendingOps: 0,
}

// Splits whatever was pasted/typed into the recovery box into words.
export function parseRecoveryWords(text: string): string[] {
  return text.toLowerCase().split(/[^a-z]+/).filter(Boolean)
}

export const useVaultStore = defineStore('vault', () => {
  const status = ref<VaultStatus>({ ...EMPTY })
  const devices = ref<VaultDevice[]>([])
  const changes = ref<Record<string, VaultChange>>({})
  const joinRequests = ref<VaultJoinRequest[]>([])
  const busy = ref(false)
  const lastError = ref('')
  const lastRevoke = ref<VaultRevokeResult | null>(null)
  // After approving: the 4 digits to type on the new device, kept on screen
  // until the user closes it.
  const confirmation = ref<{ name: string; code4: string } | null>(null)
  // Lets the user reopen the onboarding from Settings after "Not now".
  const onboardingRequested = ref(false)
  // The revoked / integrity notice can be dismissed until the next phase change.
  const noticeDismissed = ref(false)
  let started = false

  const phase = computed(() => status.value.phase)
  const isAdmin = computed(() => status.value.device.role === 'admin')
  const integrityAlert = computed(() => status.value.phase === 'error' && status.value.error?.code === 'vault.integrity')
  const onboardingVisible = computed(() => {
    const p = status.value.phase
    if (p === 'ready') return false
    if (p === 'revoked' || p === 'error') return !noticeDismissed.value
    if (p === 'off') return onboardingRequested.value || status.value.promptWelcome
    return true
  })
  // Requests closed with × stay pending but stop popping up until reopened.
  const snoozed = ref<Set<string>>(new Set())
  // Only admins see join requests (contract §3); oldest first.
  const visibleJoinRequests = computed(() => (isAdmin.value && status.value.phase === 'ready' ? joinRequests.value : []))
  const joinRequest = computed(() => visibleJoinRequests.value.find((r) => !snoozed.value.has(r.requestId)) ?? null)

  function applyStatus(s: VaultStatus) {
    const was = status.value.phase
    status.value = { ...EMPTY, ...s }
    if (s.phase !== was) noticeDismissed.value = false
    if (s.phase === 'ready') {
      onboardingRequested.value = false
      if (was !== 'ready') void refresh()
    }
    if (s.phase === 'off' && was !== 'off') {
      devices.value = []
      changes.value = {}
      joinRequests.value = []
    }
  }

  // Updates (ready, attemptsLeft) replace the request in place, keeping order.
  function addJoinRequest(r: VaultJoinRequest) {
    const i = joinRequests.value.findIndex((x) => x.requestId === r.requestId)
    joinRequests.value = i < 0 ? [...joinRequests.value, r] : joinRequests.value.map((x, k) => (k === i ? r : x))
  }

  async function start() {
    if (started) return
    started = true
    const api = getVaultApi()
    api.onStatus(applyStatus)
    api.onJoinRequest(addJoinRequest)
    api.onChanged((m) => { changes.value = m })
    try {
      applyStatus(await api.getStatus())
    } catch (e) {
      console.warn('[vault] status unavailable', e)
    }
  }

  async function refresh() {
    const api = getVaultApi()
    try {
      const [d, c, j] = await Promise.all([api.listDevices(), api.changedMap(), api.listJoinRequests()])
      devices.value = d
      changes.value = c
      j.forEach(addJoinRequest)
    } catch (e) {
      lastError.value = vaultErrorCode(e)
    }
  }

  // Runs an action, keeping `busy` and the error code in one place.
  // Resolves to `ok` so callers can branch without reading lastError.
  async function run<T>(fn: () => Promise<T>): Promise<{ ok: boolean; value?: T }> {
    busy.value = true
    lastError.value = ''
    try {
      return { ok: true, value: await fn() }
    } catch (e) {
      lastError.value = vaultErrorCode(e)
      return { ok: false }
    } finally {
      busy.value = false
    }
  }

  const api = () => getVaultApi()
  const clearError = () => { lastError.value = '' }
  const signIn = () => run(() => api().signIn())
  const cancelSignIn = () => run(() => api().cancelSignIn())
  async function skip() {
    onboardingRequested.value = false
    await run(() => api().skip())
  }
  async function recoveryWords() {
    return (await run(() => api().recoveryWords())).value ?? []
  }
  async function confirmRecovery(words: string[]) {
    return (await run(() => api().confirmRecovery(status.value.recoveryCheck ?? [], words))).ok
  }
  async function recoverWithWords(words: string[]) {
    return (await run(() => api().recoverWithWords(words))).ok
  }
  async function revokeDevice(id: string) {
    const r = await run(() => api().revokeDevice(id))
    lastRevoke.value = r.value ?? null
    await refresh()
    return r.ok
  }
  async function renameThisDevice(name: string) {
    const r = await run(() => api().renameThisDevice(name))
    if (status.value.phase === 'ready') await refresh()
    return r.ok
  }
  function snoozeJoin(id: string) {
    snoozed.value = new Set([...snoozed.value, id])
  }
  function unsnoozeJoins() {
    snoozed.value = new Set()
  }
  function dropJoinRequest(id: string) {
    joinRequests.value = joinRequests.value.filter((x) => x.requestId !== id)
  }
  async function beginApproval() {
    const r = joinRequest.value
    if (!r) return false
    const res = await run(() => api().beginApproval(r.requestId))
    if (!res.ok && isDeadRequest(lastError.value)) dropJoinRequest(r.requestId)
    return res.ok
  }
  const isDeadRequest = (code: string) => ['vault.join.tooManyAttempts', 'vault.join.expired', 'vault.join.notFound'].includes(code)
  async function approveJoin(code: string, role: VaultRole = 'admin') {
    const r = joinRequest.value
    if (!r) return false
    const res = await run(() => api().approveJoin(r.requestId, code, role))
    if (res.ok) confirmation.value = { name: r.name, code4: res.value ?? '' }
    // A dead request (attempts exhausted, expired, handled elsewhere) must leave
    // the queue; a wrong code keeps it for another try.
    if (res.ok || isDeadRequest(lastError.value)) {
      dropJoinRequest(r.requestId)
    }
    if (res.ok) await refresh()
    return res.ok
  }
  async function rejectJoin() {
    const r = joinRequest.value
    if (!r) return
    await run(() => api().rejectJoin(r.requestId))
    dropJoinRequest(r.requestId)
  }
  async function completeJoin(code4: string) {
    return (await run(() => api().completeJoin(code4))).ok
  }
  const cancelJoin = () => run(() => api().cancelJoin())
  function closeConfirmation() {
    confirmation.value = null
  }
  const canRegenerateRecovery = computed(() => !!getVaultApi().regenerateRecovery)
  async function regenerateRecovery() {
    const fn = getVaultApi().regenerateRecovery
    if (!fn) return false
    const r = await run(() => fn())
    if (r.ok && lastRevoke.value) lastRevoke.value = { ...lastRevoke.value, recoveryStale: false }
    return r.ok
  }
  const syncNow = () => run(() => api().syncNow())
  async function signOut() {
    await run(() => api().signOut())
    devices.value = []
    changes.value = {}
  }
  function openOnboarding() {
    clearError()
    onboardingRequested.value = true
  }
  function closeOnboarding() {
    onboardingRequested.value = false
    noticeDismissed.value = true
  }

  function changeFor(connectionId: string): VaultChange | null {
    return changes.value[connectionId] ?? null
  }

  return {
    status, devices, changes, joinRequests, busy, lastError, lastRevoke, confirmation,
    phase, isAdmin, integrityAlert, onboardingVisible, joinRequest, visibleJoinRequests, snoozeJoin, unsnoozeJoins,
    start, refresh, clearError, signIn, cancelSignIn, skip, recoveryWords, confirmRecovery,
    recoverWithWords, revokeDevice, renameThisDevice, beginApproval, approveJoin, rejectJoin,
    completeJoin, cancelJoin, closeConfirmation, canRegenerateRecovery, regenerateRecovery, syncNow, signOut, openOnboarding, closeOnboarding, changeFor,
  }
})

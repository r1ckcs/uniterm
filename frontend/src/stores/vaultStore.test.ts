import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useVaultStore, parseRecoveryWords } from './vaultStore'
import { createMockVaultApi, setVaultApi, vaultErrorCode, VaultError } from '../services/vaultApi'
import { VAULT_ERROR_CODES, vaultErrorText } from '../utils/vaultErrors'
import { formatChangedAt, formatCountdown, groupCode } from '../utils/vaultFormat'
import en from '../i18n/locales/en.json'

const flush = () => new Promise((r) => setTimeout(r, 0))

async function storeWith(scenario: string, ids: string[] = []) {
  setActivePinia(createPinia())
  setVaultApi(createMockVaultApi(scenario, 5, () => ids))
  const vault = useVaultStore()
  await vault.start()
  await flush()
  return vault
}

describe('vault store (mock backend)', () => {
  beforeEach(() => setVaultApi(null))

  it('first run: welcome only when promptWelcome, "Not now" hides it', async () => {
    const vault = await storeWith('welcome')
    expect(vault.onboardingVisible).toBe(true)
    await vault.skip()
    await flush()
    expect(vault.status.promptWelcome).toBe(false)
    expect(vault.onboardingVisible).toBe(false)
    vault.openOnboarding()
    expect(vault.onboardingVisible).toBe(true)
  })

  it('stays out of the way when the user never asked for it', async () => {
    const vault = await storeWith('idle')
    expect(vault.onboardingVisible).toBe(false)
  })

  it('sync-off strip: only when off, not in test mode, hidden by "Not now" for the session', async () => {
    setActivePinia(createPinia())
    setVaultApi(createMockVaultApi('idle', 5, () => []))
    const before = useVaultStore()
    expect(before.syncOffNotice).toBe(false) // no status yet
    await before.start()
    await flush()
    expect(before.syncOffNotice).toBe(true)
    before.dismissSyncOff()
    expect(before.syncOffNotice).toBe(false)
    // A new app start (fresh store) shows it again.
    expect((await storeWith('idle')).syncOffNotice).toBe(true)
    expect((await storeWith('ready')).syncOffNotice).toBe(false)
    const test = await storeWith('test')
    expect(test.status.testMode).toBe(true)
    expect(test.syncOffNotice).toBe(false)
  })

  it('first device: sign in → 24 words → confirm the 3 asked positions → ready', async () => {
    const vault = await storeWith('welcome')
    await vault.signIn()
    expect(vault.phase).toBe('signingIn')
    await vi.waitFor(() => expect(vault.phase).toBe('showRecovery'))
    const words = await vault.recoveryWords()
    expect(words).toHaveLength(24)
    const check = vault.status.recoveryCheck!
    expect(check).toHaveLength(3)

    expect(await vault.confirmRecovery(['x', 'y', 'z'])).toBe(false)
    expect(vault.lastError).toBe('vault.recovery.mismatch')
    expect(await vault.confirmRecovery(check.map((i) => words[i]))).toBe(true)
    expect(vault.phase).toBe('ready')
    expect(vault.onboardingVisible).toBe(false)
  })

  it('new device: code appears later; recovery words are an alternative', async () => {
    const vault = await storeWith('newDevice')
    expect(vault.phase).toBe('awaitingApproval')
    expect(vault.status.pendingJoin?.code6).toBeUndefined()
    expect(await vault.recoverWithWords(['abandon'])).toBe(false)
    expect(vault.lastError).toBe('vault.recovery.badWords')
  })

  it('new device: after approval, the 4 digits finish the join', async () => {
    const vault = await storeWith('newDevice')
    await vi.waitFor(() => expect(vault.status.pendingJoin?.approved).toBe(true), { timeout: 1000 })
    expect(await vault.completeJoin('0000')).toBe(false)
    expect(vault.lastError).toBe('vault.join.badCode')
    expect(vault.status.pendingJoin?.attemptsLeft).toBe(2)
    expect(await vault.completeJoin('7351')).toBe(true)
    expect(vault.phase).toBe('ready')
  })

  it('new device: cancelling the request goes back to off', async () => {
    const vault = await storeWith('newDevice')
    await vault.cancelJoin()
    expect(vault.phase).toBe('off')
  })

  it('approver: two steps, typed code, attempts and dead requests', async () => {
    const vault = await storeWith('join')
    await vi.waitFor(() => expect(vault.joinRequest).not.toBeNull())
    expect(vault.joinRequest!.ready).toBe(false)
    expect(await vault.approveJoin('482915')).toBe(false)
    expect(vault.lastError).toBe('vault.join.notReady')

    expect(await vault.beginApproval()).toBe(true)
    await vi.waitFor(() => expect(vault.joinRequest?.ready).toBe(true))
    expect(await vault.approveJoin('000000')).toBe(false)
    expect(vault.lastError).toBe('vault.join.badCode')
    await flush()
    expect(vault.joinRequest?.attemptsLeft).toBe(2)

    expect(await vault.approveJoin('482915')).toBe(true)
    expect(vault.joinRequest).toBeNull()
    // v3: the approver now shows the 4 digits to type on the new device.
    expect(vault.confirmation).toEqual({ name: 'MacBook Air da sala', code4: '7351' })
    vault.closeConfirmation()
    expect(vault.confirmation).toBeNull()
    expect(vault.devices.some((d) => d.name === 'MacBook Air da sala')).toBe(true)
  })

  it('approver: a request closed with × stops popping up until reviewed', async () => {
    const vault = await storeWith('join')
    await vi.waitFor(() => expect(vault.joinRequest).not.toBeNull())
    vault.snoozeJoin(vault.joinRequest!.requestId)
    expect(vault.joinRequest).toBeNull()
    expect(vault.visibleJoinRequests).toHaveLength(1)
    vault.unsnoozeJoins()
    expect(vault.joinRequest).not.toBeNull()
  })

  it('devices: recovery key listed, self cannot be revoked', async () => {
    const vault = await storeWith('ready')
    await vi.waitFor(() => expect(vault.devices.length).toBeGreaterThan(1))
    expect(vault.devices.find((d) => d.isRecovery)).toBeTruthy()
    const me = vault.devices.find((d) => d.isThis)!
    expect(await vault.revokeDevice(me.id)).toBe(false)
    expect(vault.lastError).toBe('vault.revoke.self')
    expect(await vault.revokeDevice('dev-mac-noc')).toBe(true)
    expect(vault.devices.some((d) => d.id === 'dev-mac-noc')).toBe(false)
    expect(vault.lastRevoke).not.toBeNull()
    // Revoking an admin makes the recovery key stale; a new one goes through
    // the same 24-word flow.
    expect(vault.lastRevoke!.recoveryStale).toBe(true)
    expect(vault.canRegenerateRecovery).toBe(true)
    expect(await vault.regenerateRecovery()).toBe(true)
    expect(vault.phase).toBe('showRecovery')
    expect(vault.status.recoveryRegen).toBe(true)
    // Cancel keeps the old phrase: the warning stays.
    await vault.cancelRecovery()
    expect(vault.phase).toBe('ready')
    expect(vault.lastRevoke!.recoveryStale).toBe(true)
    // Confirming the new phrase is what clears it.
    await vault.regenerateRecovery()
    const words = await vault.recoveryWords()
    expect(await vault.confirmRecovery(vault.status.recoveryCheck!.map((i) => words[i]))).toBe(true)
    expect(vault.phase).toBe('ready')
    expect(vault.status.recoveryRegen).toBe(false)
    expect(vault.lastRevoke!.recoveryStale).toBe(false)
  })

  it('an interrupted phrase change shows up as more than one recovery key', async () => {
    const vault = await storeWith('multiKeys')
    expect(vault.status.recoveryKeys).toBe(2)
    expect(await vault.regenerateRecovery()).toBe(true)
    const words = await vault.recoveryWords()
    expect(await vault.confirmRecovery(vault.status.recoveryCheck!.map((i) => words[i]))).toBe(true)
    expect(vault.status.recoveryKeys).toBe(1)
  })

  it('changed-by map is keyed by connection id', async () => {
    const vault = await storeWith('ready', ['a', 'b', 'c'])
    await vi.waitFor(() => expect(vault.changeFor('b')).not.toBeNull())
    expect(vault.changeFor('a')).toBeNull()
    expect(vault.changeFor('b')!.fields).toContain('host')
  })

  it('revoked and integrity notices show, and can be dismissed', async () => {
    const vault = await storeWith('error')
    expect(vault.integrityAlert).toBe(true)
    expect(vault.onboardingVisible).toBe(true)
    vault.closeOnboarding()
    expect(vault.onboardingVisible).toBe(false)
  })
})

describe('vault helpers', () => {
  it('extracts the stable error code from a rejection', () => {
    expect(vaultErrorCode(new Error('vault.join.expired: request 12 past deadline'))).toBe('vault.join.expired')
    expect(vaultErrorCode(new VaultError('vault.integrity'))).toBe('vault.integrity')
    expect(vaultErrorCode(new Error('boom'))).toBe('vault.internal')
    // Only a leading code counts; a code quoted inside a detail does not.
    expect(vaultErrorCode(new Error('wrapped: vault.integrity'))).toBe('vault.internal')
  })

  it('every contract error code has an en translation; unknown ones fall back', () => {
    const bundle = en as Record<string, string>
    for (const code of VAULT_ERROR_CODES) expect(bundle[`vault.err.${code.slice(6)}`], code).toBeTruthy()
    const t = (k: string) => bundle[k] ?? k
    expect(vaultErrorText(t, 'vault.something.new')).toBe(bundle['vault.err.internal'])
  })

  it('parses pasted recovery words', () => {
    expect(parseRecoveryWords(' 1. Abandon\n2. harbor,  velvet ')).toEqual(['abandon', 'harbor', 'velvet'])
  })

  it('formats dates and codes', () => {
    expect(formatChangedAt(new Date(2026, 9, 5, 7, 3).toISOString())).toBe('05/10 07:03')
    expect(formatChangedAt('nope')).toBe('')
    expect(formatCountdown(new Date(125_000).toISOString(), 0)).toBe('2:05')
    expect(formatCountdown(new Date(0).toISOString(), 1)).toBe('')
    expect(groupCode('482915')).toBe('482 915')
  })
})

// Device names and "changed by" come from other devices (CONTRATO §2): the
// vault screens must never feed them to v-html.
describe('vault screens', () => {
  it('use no v-html', () => {
    const sources = import.meta.glob('../components/Vault*.vue', { query: '?raw', import: 'default', eager: true }) as Record<string, string>
    expect(Object.keys(sources).length).toBeGreaterThanOrEqual(4)
    for (const [f, src] of Object.entries(sources)) expect(src, f).not.toMatch(/v-html=|\.innerHTML|dangerouslyUseHTMLString/)
  })
})

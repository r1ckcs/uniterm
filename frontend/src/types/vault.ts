// Credential vault (Google Drive sync) — UI-side types.
// Mirrors CONTRATO-cofre-f1.md §2 (the Go side has the same JSON tags).

export type VaultPhase =
  | 'off' // no account (or signed out); see promptWelcome
  | 'signingIn' // system browser open, waiting for the loopback redirect
  | 'creating' // signed in, no vault on Drive yet: generating keys (< 1 s)
  | 'showRecovery' // first device: show the 24 words, ask 3 back
  | 'awaitingApproval' // vault exists, this device is not a member yet
  | 'ready'
  | 'revoked' // this device was removed from the vault
  | 'error' // vault locked by an error (e.g. integrity); see status.error

export type VaultRole = 'admin' | 'writer' | 'reader'

export interface VaultStatus {
  phase: VaultPhase
  promptWelcome: boolean // never answered the welcome screen
  testMode: boolean // UNITERM_VAULT_STORE=file:… (no Google)
  account?: { email: string }
  device: { id: string; name: string; fingerprint: string; role?: VaultRole }
  // code6 is absent until an existing device starts the approval; approved
  // means the approver accepted it and now shows the 4 digits to type here.
  pendingJoin?: { code6?: string; fingerprint: string; expiresAt: string; approved: boolean; attemptsLeft: number }
  recoveryCheck?: number[] // 0-based positions to confirm, only in showRecovery
  // showRecovery is replacing the phrase of an open vault (Cancel allowed, the
  // old phrase stays valid until confirm), not creating the vault (CONTRATO v6).
  recoveryRegen?: boolean
  // Recovery keys in the roster (counted in ready). > 1 means a phrase change
  // was interrupted and an old phrase still works (CONTRATO v7).
  recoveryKeys?: number
  lastSyncAt?: string
  syncing: boolean
  offline: boolean
  pendingOps: number
  error?: { code: string; message: string }
}

export interface VaultDevice {
  id: string
  name: string // remote text: render as text only, never v-html
  fingerprint: string
  role: VaultRole
  isThis: boolean
  isRecovery: boolean // the 24-word recovery key: shown as such, never revocable
  lastSeenAt?: string
}

// Shown on an admin device when a new one asks to join. Deliberately carries
// no code: the approver types the one the new device shows.
export interface VaultJoinRequest {
  requestId: string
  name: string // remote text: render as text only, never v-html
  fingerprint: string
  expiresAt: string
  ready: boolean // the code is now on the new device's screen: unlock the 6-digit field
  attemptsLeft: number
}

// Last writer of a connection when it was another device.
export interface VaultChange {
  deviceId: string
  deviceName: string // remote text, idem
  changedAt: string
  fields: string[]
}

export interface VaultSyncResult { pulled: number; pushed: number; rejected: number; warnings: string[] }

export interface VaultRevokeResult {
  exposed: { connectionId: string; name: string; host: string }[]
  // An admin was revoked, so the recovery key predates the revocation and a
  // new one should be generated (CONTRATO v4).
  recoveryStale: boolean
}

export const RECOVERY_WORD_COUNT = 24

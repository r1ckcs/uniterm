// Stable vault error codes (CONTRATO-cofre-f1.md §4) → translated text.
// Unknown codes fall back to the generic message; the backend detail after
// "code:" is for the log only and never reaches the screen.
type T = (key: string, params?: Record<string, string | number>) => string

export const VAULT_ERROR_CODES = [
  'vault.oauth.cancelled', 'vault.oauth.denied', 'vault.oauth.timeout', 'vault.oauth.noClientId',
  'vault.drive.offline', 'vault.drive.auth', 'vault.drive.quota',
  'vault.state.wrongPhase', 'vault.recovery.mismatch', 'vault.recovery.badWords', 'vault.recovery.notThisVault',
  'vault.join.badCode', 'vault.join.tooManyAttempts', 'vault.join.expired', 'vault.join.notFound',
  'vault.join.notReady', 'vault.perm.notAdmin', 'vault.perm.readOnly', 'vault.revoke.self',
  'vault.integrity', 'vault.revoked', 'vault.internal',
] as const

export function vaultErrorText(t: T, code: string): string {
  if (!code) return ''
  const known = (VAULT_ERROR_CODES as readonly string[]).includes(code) ? code : 'vault.internal'
  return t(`vault.err.${known.slice('vault.'.length)}`)
}

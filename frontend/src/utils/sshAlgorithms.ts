import type { SSHAlgoConfig } from '../types/session'

// What a connection stores for its SSH algorithm preference. Compatible mode
// is the backend default and stores nothing; secure stores just the mode;
// custom keeps its four lists. legacyDevice is kept in every case (and only
// when on), so a mode change or a save never drops it.
export function normalizeSSHAlgorithms(algo: SSHAlgoConfig | undefined, sshLike: boolean): SSHAlgoConfig | undefined {
  if (!sshLike) return undefined
  const legacy = algo?.legacyDevice ? { legacyDevice: true } : {}
  const mode = algo?.mode ?? 'compatible'
  if (mode === 'compatible') return legacy.legacyDevice ? { mode: 'compatible', ...legacy } : undefined
  if (mode === 'secure') return { mode: 'secure', ...legacy }
  const { legacyDevice: _drop, ...rest } = algo!
  return { ...rest, ...legacy }
}

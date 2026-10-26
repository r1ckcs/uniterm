// Changed SSH host key (CONTRATO-chave-trocada v1.1): payload of the
// ssh:hostkey-changed event and the rules the dialog follows.

export interface HostKeyOldKey {
  keyType: string
  fingerprint: string
  file: 'app' | 'user'
}

export interface HostKeyChangedRequest {
  id: string // "" = informative only: the backend already refused
  host: string // "host:port" of this hop
  keyType: string // the NEW key
  fingerprint: string
  keyBits?: number
  weak?: boolean
  oldKeys: HostKeyOldKey[]
  canReplace: boolean
  blocker?: 'pattern'
  userFileKept?: boolean
  removeCommand?: string
  // Filled by the frontend: the connection name, when host:port finds one.
  connName?: string
}

// Only a request the backend is waiting on may be answered; the others
// just get "Close".
export function canAnswerHostKeyChanged(req: HostKeyChangedRequest): boolean {
  return !!req.id && req.canReplace
}

// Requests arrive one per hop and per dial; one dialog at a time, and the
// same id is never queued twice.
export function enqueueHostKeyChanged(queue: HostKeyChangedRequest[], req: HostKeyChangedRequest): HostKeyChangedRequest[] {
  if (req.id && queue.some((q) => q.id === req.id)) return queue
  return [...queue, { ...req, oldKeys: req.oldKeys || [] }]
}

export function hostKeyFileLabelKey(file: HostKeyOldKey['file']): string {
  return file === 'user' ? 'hostKeyChanged.fileUser' : 'hostKeyChanged.fileApp'
}

// "ssh-ed25519" → "ED25519", "ssh-rsa"/"rsa-sha2-*" → "RSA",
// "ecdsa-sha2-nistp256" → "ECDSA P-256"; unknown types upper-cased.
export function keyTypeLabel(keyType: string): string {
  const k = (keyType || '').toLowerCase()
  if (k.includes('ed25519')) return k.startsWith('sk-') ? 'ED25519-SK' : 'ED25519'
  if (k === 'ssh-rsa' || k.startsWith('rsa-sha2')) return 'RSA'
  const ec = k.match(/^(sk-)?ecdsa-sha2-nistp(\d+)/)
  if (ec) return `ECDSA${ec[1] ? '-SK' : ''} P-${ec[2]}`
  if (k === 'ssh-dss') return 'DSA'
  return (keyType || '?').replace(/^ssh-/, '').toUpperCase()
}

// The line above the fingerprints when the new key is weaker or of
// another type: "ED25519 → RSA 527 bits". Old key sizes are not in the
// payload, so a same-type size drop only shows when the new key is weak.
export function keyDowngrade(req: HostKeyChangedRequest): { from: string; to: string; weak: boolean } | null {
  const to = keyTypeLabel(req.keyType)
  const oldLabels = [...new Set((req.oldKeys || []).map((k) => keyTypeLabel(k.keyType)))]
  const typeChanged = oldLabels.length > 0 && !oldLabels.includes(to)
  if (!typeChanged && !req.weak) return null
  return {
    from: typeChanged ? oldLabels.join(' / ') : '',
    to: req.keyBits ? `${to} ${req.keyBits} bits` : to,
    weak: !!req.weak,
  }
}

// "10.20.1.1:22" / "[fe80::1]:2222" → host and port.
export function splitHostPort(hostPort: string): { host: string; port: number } {
  const m = hostPort.match(/^\[(.+)\]:(\d+)$/) || hostPort.match(/^([^:]+):(\d+)$/)
  if (!m) return { host: hostPort, port: 22 }
  return { host: m[1], port: Number(m[2]) }
}

export interface NamedTarget {
  name?: string
  host?: string
  port?: number
}

// The connection the key belongs to, by host:port (a jump host's own
// connection for a hop). Sessions being opened win over saved connections;
// two different names for the same host:port give none.
export function connectionNameFor(hostPort: string, ...sources: NamedTarget[][]): string {
  const { host, port } = splitHostPort(hostPort)
  const h = host.toLowerCase()
  for (const list of sources) {
    const names = new Set(
      list
        .filter((c) => c.name && c.host && c.host.toLowerCase() === h && (c.port || 22) === port)
        .map((c) => c.name as string),
    )
    if (names.size === 1) return [...names][0]
    if (names.size > 1) return ''
  }
  return ''
}

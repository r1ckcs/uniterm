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

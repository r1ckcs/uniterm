import { describe, expect, it } from 'vitest'
import { canAnswerHostKeyChanged, enqueueHostKeyChanged, hostKeyFileLabelKey, type HostKeyChangedRequest } from './hostKeyChanged'

const req = (over: Partial<HostKeyChangedRequest> = {}): HostKeyChangedRequest => ({
  id: 'k1', host: '10.0.0.1:22', keyType: 'ssh-ed25519', fingerprint: 'SHA256:new',
  oldKeys: [{ keyType: 'ssh-rsa', fingerprint: 'SHA256:old', file: 'app' }], canReplace: true, ...over,
})

describe('changed host key', () => {
  it('only a pending, replaceable request can be answered', () => {
    expect(canAnswerHostKeyChanged(req())).toBe(true)
    expect(canAnswerHostKeyChanged(req({ id: '' }))).toBe(false)
    expect(canAnswerHostKeyChanged(req({ canReplace: false, blocker: 'pattern' }))).toBe(false)
  })

  it('queues one dialog per request and never the same id twice', () => {
    let q = enqueueHostKeyChanged([], req())
    q = enqueueHostKeyChanged(q, req())
    q = enqueueHostKeyChanged(q, req({ id: 'k2' }))
    q = enqueueHostKeyChanged(q, req({ id: '' }))
    q = enqueueHostKeyChanged(q, req({ id: '' }))
    expect(q.map((r) => r.id)).toEqual(['k1', 'k2', '', ''])
  })

  it('tolerates a payload without oldKeys', () => {
    const q = enqueueHostKeyChanged([], { ...req(), oldKeys: undefined as unknown as [] })
    expect(q[0].oldKeys).toEqual([])
  })

  it('names the file the trusted key came from', () => {
    expect(hostKeyFileLabelKey('app')).toBe('hostKeyChanged.fileApp')
    expect(hostKeyFileLabelKey('user')).toBe('hostKeyChanged.fileUser')
  })
})

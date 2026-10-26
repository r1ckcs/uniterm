import { describe, expect, it } from 'vitest'
import { canAnswerHostKeyChanged, connectionNameFor, enqueueHostKeyChanged, hostKeyFileLabelKey, keyDowngrade, keyTypeLabel, splitHostPort, type HostKeyChangedRequest } from './hostKeyChanged'

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

describe('changed host key: name and downgrade', () => {
  it('labels key types', () => {
    expect(keyTypeLabel('ssh-ed25519')).toBe('ED25519')
    expect(keyTypeLabel('ssh-rsa')).toBe('RSA')
    expect(keyTypeLabel('rsa-sha2-512')).toBe('RSA')
    expect(keyTypeLabel('ecdsa-sha2-nistp256')).toBe('ECDSA P-256')
    expect(keyTypeLabel('ssh-dss')).toBe('DSA')
  })

  it('flags a type change and a weak new key, not a same-type change', () => {
    expect(keyDowngrade(req({ keyType: 'ssh-rsa', keyBits: 527, weak: true, oldKeys: [{ keyType: 'ssh-ed25519', fingerprint: 'x', file: 'app' }] })))
      .toEqual({ from: 'ED25519', to: 'RSA 527 bits', weak: true, sameType: false })
    expect(keyDowngrade(req({ keyType: 'ssh-ed25519', oldKeys: [{ keyType: 'ssh-ed25519', fingerprint: 'x', file: 'app' }] }))).toBeNull()
    expect(keyDowngrade(req({ keyType: 'ssh-rsa', keyBits: 768, weak: true, oldKeys: [{ keyType: 'ssh-rsa', fingerprint: 'x', file: 'user' }] })))
      .toEqual({ from: '', to: 'RSA 768 bits', weak: true, sameType: false })
  })

  it('a same-type size drop is a downgrade even when the new key is not weak', () => {
    const rsa = (bits?: number) => ({ keyType: 'ssh-rsa', fingerprint: 'x', file: 'app' as const, keyBits: bits })
    expect(keyDowngrade(req({ keyType: 'rsa-sha2-512', keyBits: 1024, oldKeys: [rsa(2048)] })))
      .toEqual({ from: 'RSA 2048 bits', to: 'RSA 1024 bits', weak: false, sameType: true })
    // The largest trusted size of that type is the reference.
    expect(keyDowngrade(req({ keyType: 'ssh-rsa', keyBits: 2048, oldKeys: [rsa(1024), rsa(4096)] }))?.from).toBe('RSA 4096 bits')
    // Same or larger size, or an old key without bits: nothing to flag.
    expect(keyDowngrade(req({ keyType: 'ssh-rsa', keyBits: 2048, oldKeys: [rsa(2048)] }))).toBeNull()
    expect(keyDowngrade(req({ keyType: 'ssh-rsa', keyBits: 4096, oldKeys: [rsa(2048)] }))).toBeNull()
    expect(keyDowngrade(req({ keyType: 'ssh-rsa', keyBits: 1024, oldKeys: [rsa()] }))).toBeNull()
    // Weak and smaller: the size drop wins the title, the weak hint stays.
    expect(keyDowngrade(req({ keyType: 'ssh-rsa', keyBits: 768, weak: true, oldKeys: [rsa(2048)] })))
      .toEqual({ from: 'RSA 2048 bits', to: 'RSA 768 bits', weak: true, sameType: true })
  })

  it('finds the connection name by host:port, sessions first, never a guess', () => {
    const saved = [{ name: 'PE01-REI', host: '10.20.2.1' }, { name: 'SW', host: '10.20.3.5', port: 2222 }, { name: 'A', host: '10.9.9.9' }, { name: 'B', host: '10.9.9.9' }]
    expect(splitHostPort('[fe80::1]:2222')).toEqual({ host: 'fe80::1', port: 2222 })
    expect(connectionNameFor('10.20.2.1:22', [], saved)).toBe('PE01-REI')
    expect(connectionNameFor('10.20.3.5:2222', [], saved)).toBe('SW')
    expect(connectionNameFor('10.20.3.5:22', [], saved)).toBe('')
    expect(connectionNameFor('10.9.9.9:22', [], saved)).toBe('')
    expect(connectionNameFor('10.9.9.9:22', [{ name: 'B', host: '10.9.9.9' }], saved)).toBe('B')
  })
})

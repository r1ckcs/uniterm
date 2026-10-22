import { describe, expect, it } from 'vitest'
import { isAuthFailureError } from './authError'

describe('isAuthFailureError', () => {
  it('matches x/crypto auth exhaustion and server rejections', () => {
    expect(isAuthFailureError('ssh handshake: ssh: unable to authenticate, attempted methods [password], no supported methods remain')).toBe(true)
    expect(isAuthFailureError('Permission denied (publickey,password)')).toBe(true)
    expect(isAuthFailureError('Permission denied, please try again.')).toBe(true)
  })

  it('is case-insensitive', () => {
    expect(isAuthFailureError('UNABLE TO AUTHENTICATE')).toBe(true)
  })

  it('rejects non-auth failures', () => {
    expect(isAuthFailureError('tcp dial: dial tcp 10.0.0.1:22: connectex: No connection could be made because the target machine actively refused it.')).toBe(false)
    expect(isAuthFailureError('ssh handshake: ssh: handshake failed: EOF')).toBe(false)
    expect(isAuthFailureError('auth timeout')).toBe(false)
    expect(isAuthFailureError('auth cancelled')).toBe(false)
    expect(isAuthFailureError('tunnel start: ssh: handshake failed: ssh: unable to authenticate')).toBe(true)
    expect(isAuthFailureError('')).toBe(false)
    expect(isAuthFailureError(undefined)).toBe(false)
  })
})

describe('handshake failures', () => {
  it('recognizes a weak legacy host key and its size', async () => {
    const { isHandshakeFailureError, legacyKeyBits } = await import('./authError')
    const ours = 'ssh handshake: ssh: legacy host key (RSA 527 bits): enable "Legacy device" for this connection'
    const raw = 'ssh handshake: ssh: handshake failed: crypto/rsa: 527-bit keys are insecure (see https://go.dev/pkg/crypto/rsa#hdr-Minimum_key_size)'
    expect(isHandshakeFailureError(ours)).toBe(true)
    expect(isHandshakeFailureError(raw)).toBe(true)
    expect(legacyKeyBits(ours)).toBe(527)
    expect(legacyKeyBits(raw)).toBe(527)
    expect(legacyKeyBits('dial tcp: timeout')).toBe(0)
  })
  it('never treats a rejected password as a handshake failure', async () => {
    const { isHandshakeFailureError } = await import('./authError')
    expect(isHandshakeFailureError('ssh handshake: ssh: handshake failed: ssh: unable to authenticate, attempted methods [none password]')).toBe(false)
    expect(isHandshakeFailureError('ssh: handshake failed: ssh: no common algorithm for key exchange')).toBe(true)
  })
})

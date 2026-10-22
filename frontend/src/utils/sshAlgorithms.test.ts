import { describe, expect, it } from 'vitest'
import { normalizeSSHAlgorithms } from './sshAlgorithms'

describe('normalizeSSHAlgorithms', () => {
  it('stores nothing for the plain compatible default', () => {
    expect(normalizeSSHAlgorithms(undefined, true)).toBeUndefined()
    expect(normalizeSSHAlgorithms({ mode: 'compatible' }, true)).toBeUndefined()
    expect(normalizeSSHAlgorithms({ mode: 'compatible', legacyDevice: false }, true)).toBeUndefined()
  })

  it('keeps legacyDevice in every mode', () => {
    expect(normalizeSSHAlgorithms({ legacyDevice: true }, true)).toEqual({ mode: 'compatible', legacyDevice: true })
    expect(normalizeSSHAlgorithms({ mode: 'secure', legacyDevice: true }, true)).toEqual({ mode: 'secure', legacyDevice: true })
    const custom = { mode: 'custom' as const, keyExchanges: ['a'], ciphers: ['b'], macs: ['c'], hostKeys: ['d'], legacyDevice: true }
    expect(normalizeSSHAlgorithms(custom, true)).toEqual(custom)
  })

  it('drops a false legacyDevice and everything for non-SSH types', () => {
    expect(normalizeSSHAlgorithms({ mode: 'secure', legacyDevice: false }, true)).toEqual({ mode: 'secure' })
    expect(normalizeSSHAlgorithms({ mode: 'secure', legacyDevice: true }, false)).toBeUndefined()
  })
})

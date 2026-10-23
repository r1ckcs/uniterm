import { describe, expect, it } from 'vitest'
import { formatLatency, latencyHintKey, latencyLevel, type LatencySample } from './latency'

const ok = (rttMs: number, via: LatencySample['via'] = 'direct'): LatencySample => ({ state: 'ok', rttMs, via })
const off = (reason: LatencySample['reason']): LatencySample => ({ state: 'unavailable', rttMs: 0, reason })

describe('footer latency', () => {
  it('formats ms with "<1 ms" below the resolution and "—" when unavailable', () => {
    expect(formatLatency(ok(0))).toBe('<1 ms')
    expect(formatLatency(ok(0.4))).toBe('0.4 ms')
    expect(formatLatency(ok(9.96))).toBe('10 ms')
    expect(formatLatency(ok(23.4))).toBe('23 ms')
    expect(formatLatency(ok(1234))).toBe('1.2 s')
    expect(formatLatency(off('mosh'))).toBe('—')
    expect(formatLatency(undefined)).toBe('—')
  })

  it('turns amber from 150 ms and red from 400 ms; gray when unavailable', () => {
    expect(latencyLevel(ok(149))).toBe('ok')
    expect(latencyLevel(ok(150))).toBe('warn')
    expect(latencyLevel(ok(399))).toBe('warn')
    expect(latencyLevel(ok(400))).toBe('alert')
    expect(latencyLevel(off('type'))).toBe('off')
  })

  it('says what was measured: host, proxy or the first jump', () => {
    expect(latencyHintKey(ok(5))).toBe('terminal.latencyDirect')
    expect(latencyHintKey(ok(5, 'proxy'))).toBe('terminal.latencyProxy')
    expect(latencyHintKey(ok(5, 'jump'))).toBe('terminal.latencyJump')
    expect(latencyHintKey(off('closed'))).toBe('terminal.latencyUnavailable')
  })
})

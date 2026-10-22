import { describe, expect, it } from 'vitest'
import { formatUptime, statusBarTarget } from './terminalStatus'
import type { ConnectionConfig } from '../types/session'

const cfg = (c: Partial<ConnectionConfig>) => c as ConnectionConfig

describe('terminal status bar', () => {
  it('formats uptime compactly', () => {
    expect(formatUptime(12_000)).toBe('12s')
    expect(formatUptime(307_000)).toBe('5m 07s')
    expect(formatUptime(3_900_000)).toBe('1h 05m')
    expect(formatUptime(2 * 86_400_000 + 3 * 3_600_000)).toBe('2d 03h')
  })

  it('shows host:port only for a non-default port, brackets IPv6', () => {
    const label = () => 'zsh'
    expect(statusBarTarget('ssh', cfg({ host: '10.20.1.1', port: 22 }), label)).toBe('10.20.1.1')
    expect(statusBarTarget('telnet', cfg({ host: '10.20.1.10', port: 2323 }), label)).toBe('10.20.1.10:2323')
    expect(statusBarTarget('ssh', cfg({ host: '2001:db8::1', port: 2222 }), label)).toBe('[2001:db8::1]:2222')
    expect(statusBarTarget('serial', cfg({ serialPort: '/dev/tty.usbserial', serialBaudRate: 9600 }), label)).toBe('/dev/tty.usbserial · 9600 bd')
    expect(statusBarTarget('local', cfg({ shellPath: '/bin/zsh' }), label)).toBe('zsh')
  })
})

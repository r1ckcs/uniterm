import type { ConnectionConfig } from '../types/session'

// "12s", "5m 07s", "1h 05m", "2d 03h": compact uptime for the status bar.
export function formatUptime(ms: number): string {
  const s = Math.max(0, Math.floor(ms / 1000))
  const pad = (n: number) => String(n).padStart(2, '0')
  if (s < 60) return `${s}s`
  const m = Math.floor(s / 60)
  if (m < 60) return `${m}m ${pad(s % 60)}s`
  const h = Math.floor(m / 60)
  if (h < 24) return `${h}h ${pad(m % 60)}m`
  return `${Math.floor(h / 24)}d ${pad(h % 24)}h`
}

const DEFAULT_PORTS: Record<string, number> = { ssh: 22, telnet: 23, mosh: 22 }

// What the session points at: host:port (port only when not the default),
// the serial device with its baud rate, or the local shell.
export function statusBarTarget(type: string, config: ConnectionConfig | null, shellLabel: (path: string) => string): string {
  if (!config) return ''
  if (type === 'serial') return [config.serialPort, config.serialBaudRate ? `${config.serialBaudRate} bd` : ''].filter(Boolean).join(' · ')
  if (type === 'local' || type === 'wsl') return shellLabel(config.shellPath || '')
  if (!config.host) return ''
  const port = config.port && config.port !== DEFAULT_PORTS[type] ? `:${config.port}` : ''
  const host = config.host.includes(':') && port ? `[${config.host}]` : config.host
  return `${host}${port}`
}

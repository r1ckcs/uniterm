// Session latency for the terminal footer (CONTRATO-latencia v1).

export interface LatencySample {
  state: 'ok' | 'unavailable'
  rttMs: number
  via?: 'direct' | 'proxy' | 'jump'
  at?: string
  reason?: 'type' | 'mosh' | 'platform' | 'connecting' | 'closed' | 'error'
}

export type LatencyLevel = 'ok' | 'warn' | 'alert' | 'off'

// Footer colors: amber from 150 ms, red from 400 ms.
export const LATENCY_WARN_MS = 150
export const LATENCY_ALERT_MS = 400

export function latencyLevel(s: LatencySample | undefined): LatencyLevel {
  if (!s || s.state !== 'ok') return 'off'
  if (s.rttMs >= LATENCY_ALERT_MS) return 'alert'
  if (s.rttMs >= LATENCY_WARN_MS) return 'warn'
  return 'ok'
}

// "<1 ms", "0.4 ms", "23 ms", "1.2 s"; "—" when unavailable. 0 means below
// the kernel's resolution.
export function formatLatency(s: LatencySample | undefined): string {
  if (!s || s.state !== 'ok') return '—'
  const ms = s.rttMs
  if (!(ms > 0)) return '<1 ms'
  if (ms < 10) return `${Math.round(ms * 10) / 10} ms`
  if (ms < 1000) return `${Math.round(ms)} ms`
  return `${Math.round(ms / 100) / 10} s`
}

// i18n key of the tooltip: what the number measures, or why there is none.
export function latencyHintKey(s: LatencySample | undefined): string {
  if (!s || s.state !== 'ok') return 'terminal.latencyUnavailable'
  if (s.via === 'jump') return 'terminal.latencyJump'
  if (s.via === 'proxy') return 'terminal.latencyProxy'
  return 'terminal.latencyDirect'
}

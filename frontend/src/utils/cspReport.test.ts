import { describe, it, expect, vi } from 'vitest'
import { formatViolation, installCspReporter } from './cspReport'

function violation(over: Partial<Record<string, unknown>> = {}): Event {
  const ev = new Event('securitypolicyviolation')
  Object.assign(ev, {
    effectiveDirective: 'script-src-elem', blockedURI: 'inline', sourceFile: 'wails://localhost/', lineNumber: 3,
    disposition: 'enforce', sample: 'secret terminal text', ...over,
  })
  return ev
}

describe('cspReport', () => {
  it('formats directive and source, never the sample', () => {
    const msg = formatViolation(violation() as unknown as SecurityPolicyViolationEvent)
    expect(msg).toBe('enforce script-src-elem: inline at wails://localhost/:3')
    expect(msg).not.toContain('secret')
  })

  it('logs each distinct violation once', async () => {
    const log = vi.fn()
    const target = new EventTarget()
    installCspReporter(log, target)
    target.dispatchEvent(violation())
    target.dispatchEvent(violation())
    target.dispatchEvent(violation({ effectiveDirective: 'connect-src', blockedURI: 'https://evil.example' }))
    expect(log).toHaveBeenCalledTimes(2)
    expect(log.mock.calls[1]).toEqual(['csp', 'enforce connect-src: https://evil.example at wails://localhost/:3'])
  })

  it('caps the number of reports', () => {
    const log = vi.fn()
    const target = new EventTarget()
    installCspReporter(log, target)
    for (let i = 0; i < 80; i++) target.dispatchEvent(violation({ lineNumber: i }))
    expect(log).toHaveBeenCalledTimes(50)
  })
})

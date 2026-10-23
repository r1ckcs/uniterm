// Writes Content-Security-Policy violations (policy in csp.go) to the app
// log, so a resource the policy blocks shows up there instead of failing
// silently. Only the directive and where it came from are logged, never the
// blocked content's sample (it can be terminal or AI text).

const MAX_REPORTS = 50

export function formatViolation(e: Pick<SecurityPolicyViolationEvent,
  'effectiveDirective' | 'blockedURI' | 'sourceFile' | 'lineNumber' | 'disposition'>): string {
  const uri = (e.blockedURI || '(inline)').slice(0, 120)
  const where = e.sourceFile ? ` at ${e.sourceFile.slice(0, 120)}:${e.lineNumber}` : ''
  return `${e.disposition} ${e.effectiveDirective}: ${uri}${where}`
}

export function installCspReporter(log: (tag: string, message: string) => unknown, target: EventTarget = document): void {
  const seen = new Set<string>()
  target.addEventListener('securitypolicyviolation', (ev) => {
    const msg = formatViolation(ev as SecurityPolicyViolationEvent)
    if (seen.has(msg) || seen.size >= MAX_REPORTS) return
    seen.add(msg)
    console.warn('[csp]', msg)
    Promise.resolve(log('csp', msg)).catch(() => {})
  })
}

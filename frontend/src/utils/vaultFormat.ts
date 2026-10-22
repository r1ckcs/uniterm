// Dates for the vault screens. "Changed by" uses a fixed DD/MM HH:MM (the
// format the NOC reads everywhere); device "last seen" is relative.

const pad = (n: number) => String(n).padStart(2, '0')

export function formatChangedAt(iso: string): string {
  const d = new Date(iso)
  if (isNaN(d.getTime())) return ''
  return `${pad(d.getDate())}/${pad(d.getMonth() + 1)} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

export function formatLastSeen(iso: string, locale: string, now = Date.now()): string {
  const t = new Date(iso).getTime()
  if (!iso || isNaN(t)) return ''
  const sec = Math.round((t - now) / 1000)
  const rtf = new Intl.RelativeTimeFormat(locale, { numeric: 'auto' })
  const abs = Math.abs(sec)
  if (abs < 60) return rtf.format(0, 'second')
  if (abs < 3600) return rtf.format(Math.round(sec / 60), 'minute')
  if (abs < 86400) return rtf.format(Math.round(sec / 3600), 'hour')
  return rtf.format(Math.round(sec / 86400), 'day')
}

// "482915" → "482 915" for reading aloud / comparing at a glance.
export function groupCode(code: string): string {
  return code.length === 6 ? `${code.slice(0, 3)} ${code.slice(3)}` : code
}

// "9:41" until `iso`; '' once expired.
export function formatCountdown(iso: string, now = Date.now()): string {
  const left = Math.floor((new Date(iso).getTime() - now) / 1000)
  if (!(left > 0)) return ''
  return `${Math.floor(left / 60)}:${pad(left % 60)}`
}

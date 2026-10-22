// Terminal line height: xterm's lineHeight multiplier. Absent means 1.0 (what
// every install had before the setting existed); new installs get 1.1 from
// the Go defaults.
export const LINE_HEIGHT_MIN = 1
export const LINE_HEIGHT_MAX = 1.5
export const LINE_HEIGHT_STEP = 0.05

export function terminalLineHeight(v: number | undefined | null): number {
  if (typeof v !== 'number' || !Number.isFinite(v)) return 1
  const stepped = Math.round(v / LINE_HEIGHT_STEP) * LINE_HEIGHT_STEP
  return Math.min(LINE_HEIGHT_MAX, Math.max(LINE_HEIGHT_MIN, Number(stepped.toFixed(2))))
}

export interface FontEntry { Name: string; IsMono: boolean }

// Platform monospace faces worth surfacing right after the bundled JetBrains
// Mono, in this order, when the system has them.
const RECOMMENDED = ['SF Mono', 'Menlo', 'Cascadia Code', 'Cascadia Mono']

// The picker's font list built from GetAllFonts (bundled font first, then
// every installed family):
// - names starting with "." are hidden system faces the webview cannot load
//   by name, so they are dropped;
// - macOS ships SF Mono only as the hidden ".SF NS Mono"; it is offered as
//   "SF Mono", which formatFontFamily backs with ui-monospace;
// - the recommended faces move up, right after the bundled one.
export function terminalFontChoices(fonts: FontEntry[]): FontEntry[] {
  if (fonts.length === 0) return fonts
  const [bundled, ...rest] = fonts
  const visible = rest.filter(f => f.Name && !f.Name.startsWith('.'))
  const names = new Set(visible.map(f => f.Name))
  const hasSfMono = names.has('SF Mono') || rest.some(f => f.Name === '.SF NS Mono')
  const pinned: FontEntry[] = []
  for (const name of RECOMMENDED) {
    if (name === 'SF Mono' ? hasSfMono : names.has(name)) pinned.push({ Name: name, IsMono: true })
  }
  const pinnedNames = new Set(pinned.map(f => f.Name))
  return [bundled, ...pinned, ...visible.filter(f => !pinnedNames.has(f.Name))]
}

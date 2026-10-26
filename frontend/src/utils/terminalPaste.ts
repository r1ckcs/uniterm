// Paste payload preparation + write/scroll for an SSH/local session.
//
// Extracted from BaseTerminal.pasteToSession so the paste behavior is
// unit-testable (the component itself is not mountable in vitest).

export interface PasteTarget {
  /** Already-known bracketed-paste mode of the target session's terminal. */
  bracketedPasteMode: boolean
  /** Write the final payload to the session. */
  write: (payload: string) => void
  /** Yank the viewport back to the bottom after writing. */
  scrollToBottom: () => void
}

/** Normalize line endings: \r\n -> \n, then drop leftover \r (Windows
 * clipboard). Keeps multi-line pastes from doubling newlines in vim. */
export function normalizePastedText(text: string): string {
  return text.replace(/\r\n/g, '\n').replace(/\r/g, '')
}

/** Wrap in bracketed-paste markers when the app enabled the mode, so vim etc.
 * don't re-indent each pasted line. Every control character but tab and
 * line breaks is dropped from the text first (C0, DEL and C1, as kitty
 * does): a pasted "ESC[201~" would end the bracket early and run the lines
 * after it (item 52), and ^C/^Z/^\ would still raise a signal on the remote
 * tty and cut the paste short (item 53). Without the mode there is no
 * bracket, and the text goes as is. */
export function bracketPaste(text: string, enabled: boolean): string {
  return enabled ? `\x1b[200~${text.replace(/[\x00-\x08\x0b\x0c\x0e-\x1f\x7f-\x9f]/g, '')}\x1b[201~` : text
}

/** Control characters as visible symbols for the paste preview: C0 as the
 * Unicode control pictures (ESC → ␛, BEL → ␇), DEL → ␡, C1 as ⟨9B⟩.
 * Tabs stay tabs. */
export function visibleControls(line: string): string {
  return line.replace(/[\x00-\x08\x0a-\x1f\x7f-\x9f]/g, (c) => {
    const code = c.charCodeAt(0)
    if (code < 0x20) return String.fromCharCode(0x2400 + code)
    if (code === 0x7f) return '\u2421'
    return `⟨${code.toString(16).toUpperCase()}⟩`
  })
}

// Paste a block of text into one session: normalize + bracket-wrap, write to
// the session, then yank the viewport back to the bottom. Pasted input must
// behave like keyboard input — Enter/arrows already auto-scroll via xterm's
// scrollOnUserInput, but paste bypasses the keydown path (issue 629).
export function pasteWithScroll(target: PasteTarget, text: string): void {
  const payload = bracketPaste(normalizePastedText(text), target.bracketedPasteMode)
  target.write(payload)
  target.scrollToBottom()
}
// ── Broadcast paste confirmation (item 48) ──
// A multi-line paste with broadcast on reaches every target session at
// once (a 40-line config into 4 OLTs). It waits for an explicit "Paste into
// N sessions"; one line, or a single target, goes as before.

export interface NamedPasteTarget extends PasteTarget {
  name: string
  host: string
}

export interface BroadcastPasteRequest {
  lineCount: number
  sessions: { name: string; host: string }[]
  preview: string[] // first lines, for the dialog
  more: number // lines left out of the preview
}

export const BROADCAST_PASTE_PREVIEW_LINES = 6

// Lines the paste types: one trailing newline (a copied line with its
// Enter) still counts as one line.
export function pastedLineCount(text: string): number {
  const t = normalizePastedText(text).replace(/\n$/, '')
  return t ? t.split('\n').length : 0
}

export function needsBroadcastPasteConfirm(text: string, targetCount: number): boolean {
  return targetCount > 1 && pastedLineCount(text) > 1
}

export function broadcastPasteRequest(text: string, targets: { name: string; host: string }[]): BroadcastPasteRequest {
  const lines = normalizePastedText(text).replace(/\n$/, '').split('\n')
  const preview = lines.slice(0, BROADCAST_PASTE_PREVIEW_LINES).map(visibleControls)
  return {
    lineCount: lines.length,
    sessions: targets.map(({ name, host }) => ({ name, host })),
    preview,
    more: lines.length - preview.length,
  }
}

// Paste into every target, asking first when needsBroadcastPasteConfirm.
// Cancel writes nothing anywhere; confirm writes each target once.
export async function pasteToTargets(
  text: string,
  targets: NamedPasteTarget[],
  confirm: (req: BroadcastPasteRequest) => Promise<boolean>,
): Promise<boolean> {
  if (!targets.length) return false
  if (needsBroadcastPasteConfirm(text, targets.length)) {
    if (!(await confirm(broadcastPasteRequest(text, targets)))) return false
  }
  for (const target of targets) pasteWithScroll(target, text)
  return true
}

// The same targets, each write led by prefix: an AI suggestion clears the
// line (^U) right before its text, and only once the paste is confirmed, so
// a cancel leaves every line as it was.
export function withWritePrefix(targets: NamedPasteTarget[], prefix: string): NamedPasteTarget[] {
  return targets.map((t) => ({
    name: t.name,
    host: t.host,
    get bracketedPasteMode() { return t.bracketedPasteMode },
    write: (payload: string) => t.write(prefix + payload),
    scrollToBottom: () => t.scrollToBottom(),
  }))
}

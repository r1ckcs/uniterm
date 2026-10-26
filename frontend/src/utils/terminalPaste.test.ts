import { describe, it, expect, vi } from 'vitest'
import { normalizePastedText, bracketPaste, pasteWithScroll, pastedLineCount, needsBroadcastPasteConfirm, pasteToTargets, type BroadcastPasteRequest } from './terminalPaste'
import type { PasteTarget } from './terminalPaste'

function mkTarget(overrides: Partial<PasteTarget> = {}): PasteTarget & { scrollToBottom: ReturnType<typeof vi.fn> } {
  return {
    bracketedPasteMode: false,
    write: vi.fn(),
    scrollToBottom: vi.fn(),
    ...overrides,
  }
}

describe('normalizePastedText', () => {
  it('converts CRLF to LF and strips stray CR', () => {
    expect(normalizePastedText('a\r\nb\r\nc')).toBe('a\nb\nc')
    expect(normalizePastedText('a\rb')).toBe('ab')
  })

  it('leaves plain text untouched', () => {
    expect(normalizePastedText('ls -la')).toBe('ls -la')
  })
})

describe('bracketPaste', () => {
  it('wraps when bracketed-paste mode is on', () => {
    expect(bracketPaste('vim', true)).toBe('\x1b[200~vim\x1b[201~')
  })

  it('passes through when bracketed-paste mode is off', () => {
    expect(bracketPaste('vim', false)).toBe('vim')
  })
})

describe('pasteWithScroll', () => {
  it('writes the normalized, wrapped payload to the session', () => {
    const t = mkTarget({ bracketedPasteMode: true })
    pasteWithScroll(t, 'a\r\nb')
    expect(t.write).toHaveBeenCalledWith('\x1b[200~a\nb\x1b[201~')
  })

  // The regression under test (issue 629): pasting while scrolled up must
  // yank the viewport back to the bottom, mirroring how Enter/arrow keys
  // auto-scroll via xterm's scrollOnUserInput.
  it('scrolls the viewport to the bottom after writing pasted text', () => {
    const t = mkTarget()
    pasteWithScroll(t, 'printf "big log\\n"')
    expect(t.scrollToBottom).toHaveBeenCalled()
  })
})
describe('broadcast paste confirmation (item 48)', () => {
  const target = (name: string, bracketed = false) => {
    const writes: string[] = []
    return {
      writes,
      t: { name, host: `10.0.0.${name.length}`, bracketedPasteMode: bracketed, write: (p: string) => { writes.push(p) }, scrollToBottom: () => {} },
    }
  }
  const config = Array.from({ length: 40 }, (_, i) => `line ${i + 1}`).join('\r\n') + '\r\n'

  it('counts typed lines; one trailing Enter is still one line', () => {
    expect(pastedLineCount('show version\n')).toBe(1)
    expect(pastedLineCount('a\r\nb')).toBe(2)
    expect(pastedLineCount(config)).toBe(40)
    expect(needsBroadcastPasteConfirm('a\nb', 1)).toBe(false)
    expect(needsBroadcastPasteConfirm('a\n', 4)).toBe(false)
    expect(needsBroadcastPasteConfirm('a\nb', 2)).toBe(true)
  })

  it('multi-line into several sessions asks with lines, sessions and a preview', async () => {
    const olts = ['OLT1', 'OLT2', 'OLT3', 'OLT4'].map((n) => target(n))
    const confirm = vi.fn(async () => true)
    expect(await pasteToTargets(config, olts.map((o) => o.t), confirm)).toBe(true)
    expect(confirm).toHaveBeenCalledTimes(1)
    const req = confirm.mock.calls[0][0] as unknown as BroadcastPasteRequest
    expect(req.lineCount).toBe(40)
    expect(req.sessions.map((s) => s.name)).toEqual(['OLT1', 'OLT2', 'OLT3', 'OLT4'])
    expect(req.preview).toEqual(['line 1', 'line 2', 'line 3', 'line 4', 'line 5', 'line 6'])
    expect(req.more).toBe(34)
    // Confirm: every session gets the paste exactly once.
    for (const o of olts) {
      expect(o.writes).toHaveLength(1)
      expect(o.writes[0]).toBe(normalizePastedText(config))
    }
  })

  it('cancel sends nothing to any session', async () => {
    const olts = ['A', 'B', 'C'].map((n) => target(n, true))
    expect(await pasteToTargets('conf t\ninterface gpon 0/1\n', olts.map((o) => o.t), async () => false)).toBe(false)
    for (const o of olts) expect(o.writes).toEqual([])
  })

  it('one line, or broadcast off (a single target), pastes as before without asking', async () => {
    const confirm = vi.fn(async () => false)
    const many = ['A', 'B'].map((n) => target(n))
    await pasteToTargets('show run\n', many.map((o) => o.t), confirm)
    const single = target('self', true)
    await pasteToTargets('a\nb\nc', [single.t], confirm)
    expect(confirm).not.toHaveBeenCalled()
    for (const o of many) expect(o.writes).toEqual(['show run\n'])
    expect(single.writes).toEqual(['\x1b[200~a\nb\nc\x1b[201~'])
  })

  it('reads each session paste mode when writing, after the dialog', async () => {
    let mode = false
    const writes: string[] = []
    const a = { name: 'A', host: '', get bracketedPasteMode() { return mode }, write: (p: string) => { writes.push(p) }, scrollToBottom: () => {} }
    const b = target('B')
    await pasteToTargets('x\ny', [a, b.t], async () => { mode = true; return true })
    expect(writes).toEqual(['\x1b[200~x\ny\x1b[201~'])
  })
})

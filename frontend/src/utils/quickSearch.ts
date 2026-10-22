// The quick connection search shortcut: ⌘K on macOS, Ctrl+Shift+P elsewhere.
// It is a fixed platform binding, outside the rebindable shortcuts: those
// answer to Ctrl and Cmd alike, and Ctrl+K is the shell's kill-line.
export function isQuickSearchShortcut(e: Pick<KeyboardEvent, 'key' | 'metaKey' | 'ctrlKey' | 'shiftKey' | 'altKey'>, isMac: boolean): boolean {
  const key = e.key.toLowerCase()
  if (isMac) return e.metaKey && !e.ctrlKey && !e.shiftKey && !e.altKey && key === 'k'
  return e.ctrlKey && e.shiftKey && !e.altKey && !e.metaKey && key === 'p'
}

export function quickSearchLabel(isMac: boolean): string {
  return isMac ? '⌘K' : 'Ctrl+Shift+P'
}

// Payload of the 'app:quick-search' window event sent to the start tab.
export interface QuickSearchRequest {
  tabId: string
  // Tab to go back to on Esc (null when the search was already in front).
  returnTo: string | null
  // The start tab was opened just for the search: Esc closes it.
  created: boolean
}

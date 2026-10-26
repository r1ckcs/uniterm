// The ⌘K session picker (item 44): open sessions first, then saved
// connections. The "…" tab menu uses the same list in 'tabs' mode (every
// tab, no saved connections). Pure data so it can be tested.
import type { ConnectionConfig, SessionStatus } from '../types/session'
import type { Panel, Tab } from '../types/workspace'
import { statusBarTarget } from './terminalStatus'

export type PickerMode = 'all' | 'tabs'
export type PickerState = 'connected' | 'connecting' | 'down'

export interface SessionItem {
  kind: 'session'
  key: string
  tabId: string
  panelId: string
  name: string
  protocol: string
  host: string
  state: PickerState
  latencyMs?: number
  broadcasting: boolean
  // Set when the panel lives in a workspace tab.
  workspace?: string
}

export interface SavedItem {
  kind: 'saved'
  key: string
  config: ConnectionConfig
  name: string
  protocol: string
  host: string
}

// Tabs without a session (start page, settings), only in 'tabs' mode.
export interface TabItem {
  kind: 'tab'
  key: string
  tabId: string
  name: string
}

export type PickerItem = SessionItem | SavedItem | TabItem

export interface PickerSources {
  tabs: Tab[]
  getPanel: (id: string) => Panel | undefined
  sessionStatus: (sessionId: string) => SessionStatus | undefined
  latencyMs?: (sessionId: string) => number | undefined
  isBroadcasting: (panelId: string) => boolean
  connections: ConnectionConfig[]
  shellLabel?: (path: string) => string
}

export interface PickerGroups {
  sessions: SessionItem[]
  saved: SavedItem[]
  tabs: TabItem[]
}

function stateOf(panel: Panel, src: PickerSources): PickerState {
  const s = (panel.sessionId && src.sessionStatus(panel.sessionId)) || panel.status
  if (s === 'connected') return 'connected'
  if (s === 'connecting') return 'connecting'
  return 'down'
}

function sessionItem(tab: Tab, panel: Panel, src: PickerSources, workspace?: string): SessionItem {
  const name = workspace ? panel.title || panel.config?.name || tab.name : tab.name || panel.title
  const latency = panel.sessionId ? src.latencyMs?.(panel.sessionId) : undefined
  return {
    kind: 'session',
    key: `s:${panel.id}`,
    tabId: tab.id,
    panelId: panel.id,
    name: name || panel.type,
    protocol: panel.type.toUpperCase(),
    host: statusBarTarget(panel.type, panel.config, src.shellLabel || ((p) => p || 'Local')),
    state: stateOf(panel, src),
    ...(latency !== undefined ? { latencyMs: latency } : {}),
    broadcasting: src.isBroadcasting(panel.id),
    ...(workspace ? { workspace } : {}),
  }
}

export function buildPickerGroups(src: PickerSources, mode: PickerMode): PickerGroups {
  const sessions: SessionItem[] = []
  const tabs: TabItem[] = []
  for (const tab of src.tabs) {
    if (tab.type === 'workspace') {
      for (const id of tab.panelIds) {
        const p = src.getPanel(id)
        if (p) sessions.push(sessionItem(tab, p, src, tab.name))
      }
      continue
    }
    const panel = tab.type !== 'settings' && 'panelId' in tab ? src.getPanel(tab.panelId) : undefined
    if (panel) sessions.push(sessionItem(tab, panel, src))
    else if (mode === 'tabs') tabs.push({ kind: 'tab', key: `t:${tab.id}`, tabId: tab.id, name: tab.name })
  }
  const saved: SavedItem[] = mode === 'all'
    ? src.connections.map((c) => ({
        kind: 'saved' as const,
        key: `c:${c.id}`,
        config: c,
        name: c.name,
        protocol: c.type.toUpperCase(),
        host: statusBarTarget(c.type, c, src.shellLabel || ((p) => p || 'Local')),
      }))
    : []
  return { sessions, saved, tabs }
}

// Any part of the name or the host, case-insensitive ("BORDA", "10.20.2").
export function pickerMatches(item: PickerItem, query: string): boolean {
  const q = query.trim().toLowerCase()
  if (!q) return true
  const hay = [item.name, 'host' in item ? item.host : '', item.kind === 'session' ? item.workspace || '' : '']
  return hay.some((s) => s.toLowerCase().includes(q))
}

export function filterPickerGroups(g: PickerGroups, query: string): PickerGroups {
  return {
    sessions: g.sessions.filter((i) => pickerMatches(i, query)),
    saved: g.saved.filter((i) => pickerMatches(i, query)),
    tabs: g.tabs.filter((i) => pickerMatches(i, query)),
  }
}

// Keyboard order: open sessions, other tabs, saved connections.
export function flattenPickerGroups(g: PickerGroups): PickerItem[] {
  return [...g.sessions, ...g.tabs, ...g.saved]
}

// Arrow keys wrap around; an empty list has no active item.
export function movePickerIndex(index: number, delta: number, length: number): number {
  if (length <= 0) return -1
  if (index < 0) return delta > 0 ? 0 : length - 1
  return (index + delta + length) % length
}

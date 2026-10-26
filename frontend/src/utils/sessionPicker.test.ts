import { describe, expect, it } from 'vitest'
import type { Panel, Tab } from '../types/workspace'
import type { ConnectionConfig } from '../types/session'
import { buildPickerGroups, filterPickerGroups, flattenPickerGroups, movePickerIndex, type PickerSources } from './sessionPicker'

const panel = (id: string, name: string, host: string, sessionId?: string, type = 'ssh'): Panel =>
  ({ id, type, title: name, status: 'disconnected', sessionId, config: { id: `c-${id}`, name, host, type } } as unknown as Panel)

function sources(): PickerSources {
  const panels: Record<string, Panel> = {
    p1: panel('p1', 'MK-BORDA01', '10.20.2.1', 's1'),
    p2: panel('p2', 'OLT-PJT01', '10.20.1.10', 's2', 'telnet'),
    p3: panel('p3', 'PE01-REI', '10.20.2.5', undefined),
    w1: panel('w1', 'SW-CORE', '10.30.0.1', 's4'),
  }
  const tabs = [
    { id: 't0', type: 'start', name: 'Nova aba' },
    { id: 't1', type: 'terminal', name: 'MK-BORDA01', panelId: 'p1' },
    { id: 't2', type: 'terminal', name: 'OLT-PJT01', panelId: 'p2' },
    { id: 't3', type: 'terminal', name: 'PE01-REI', panelId: 'p3' },
    { id: 'tw', type: 'workspace', name: 'Núcleo', panelIds: ['w1'], activePanelId: 'w1' },
    { id: 'ts', type: 'settings', name: 'Configurações', panelId: 'settings' },
  ] as unknown as Tab[]
  const status: Record<string, 'connected' | 'connecting'> = { s1: 'connected', s2: 'connecting', s4: 'connected' }
  return {
    tabs,
    getPanel: (id) => panels[id],
    sessionStatus: (sid) => status[sid],
    latencyMs: (sid) => (sid === 's1' ? 23 : undefined),
    isBroadcasting: (pid) => pid === 'p1',
    connections: [
      { id: 'c9', name: 'BORDA-02', host: '177.200.160.2', port: 22, type: 'ssh' },
      { id: 'c8', name: 'zabbix', host: '181.191.196.24', type: 'ssh' },
    ] as unknown as ConnectionConfig[],
  }
}

describe('session picker', () => {
  it('lists open sessions (workspace panels too) with state, latency and broadcast', () => {
    const g = buildPickerGroups(sources(), 'all')
    expect(g.sessions.map((s) => [s.name, s.state])).toEqual([
      ['MK-BORDA01', 'connected'], ['OLT-PJT01', 'connecting'], ['PE01-REI', 'down'], ['SW-CORE', 'connected'],
    ])
    expect(g.sessions[0]).toMatchObject({ protocol: 'SSH', host: '10.20.2.1', latencyMs: 23, broadcasting: true })
    expect(g.sessions[1]).toMatchObject({ protocol: 'TELNET', broadcasting: false })
    expect(g.sessions[1].latencyMs).toBeUndefined()
    expect(g.sessions[3]).toMatchObject({ tabId: 'tw', panelId: 'w1', workspace: 'Núcleo' })
    expect(g.tabs).toEqual([])
    expect(g.saved.map((s) => s.name)).toEqual(['BORDA-02', 'zabbix'])
  })

  it('puts open sessions before saved connections', () => {
    const flat = flattenPickerGroups(filterPickerGroups(buildPickerGroups(sources(), 'all'), 'borda'))
    expect(flat.map((i) => [i.kind, i.name])).toEqual([['session', 'MK-BORDA01'], ['saved', 'BORDA-02']])
  })

  it('matches any part of the name or the host', () => {
    const g = buildPickerGroups(sources(), 'all')
    expect(flattenPickerGroups(filterPickerGroups(g, '10.20.2')).map((i) => i.name)).toEqual(['MK-BORDA01', 'PE01-REI'])
    expect(flattenPickerGroups(filterPickerGroups(g, 'pjt')).map((i) => i.name)).toEqual(['OLT-PJT01'])
    expect(flattenPickerGroups(filterPickerGroups(g, '196.24')).map((i) => i.name)).toEqual(['zabbix'])
    expect(flattenPickerGroups(filterPickerGroups(g, 'núcleo')).map((i) => i.name)).toEqual(['SW-CORE'])
    expect(flattenPickerGroups(filterPickerGroups(g, 'nada-disso'))).toEqual([])
  })

  it('tabs mode: every tab, no saved connections', () => {
    const g = buildPickerGroups(sources(), 'tabs')
    expect(g.saved).toEqual([])
    expect(g.tabs.map((t) => t.name)).toEqual(['Nova aba', 'Configurações'])
    expect(flattenPickerGroups(g).length).toBe(6)
  })

  it('arrow keys wrap around the list', () => {
    expect(movePickerIndex(-1, 1, 5)).toBe(0)
    expect(movePickerIndex(-1, -1, 5)).toBe(4)
    expect(movePickerIndex(4, 1, 5)).toBe(0)
    expect(movePickerIndex(0, -1, 5)).toBe(4)
    expect(movePickerIndex(2, 1, 0)).toBe(-1)
  })
})

describe('session picker markup (a11y)', () => {
  const raw = import.meta.glob(['../components/SessionPicker.vue', '../components/TabsList.vue'], { query: '?raw', import: 'default', eager: true }) as Record<string, string>
  const picker = raw['../components/SessionPicker.vue']
  const bar = raw['../components/TabsList.vue']

  it('is a combobox driving a listbox, with the active option announced', () => {
    expect(picker).toMatch(/role="combobox"/)
    expect(picker).toMatch(/role="listbox"/)
    expect(picker).toMatch(/role="option"/)
    expect(picker).toMatch(/:aria-selected=/)
    expect(picker).toMatch(/:aria-activedescendant=/)
  })

  it('the tab overflow is a real button with a label', () => {
    expect(bar).toMatch(/<button\s+type="button"\s+class="tab-more-btn"[^>]*:aria-label=/)
    expect(bar).not.toMatch(/<span class="tab-more-btn"/)
  })
})

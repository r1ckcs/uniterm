import { beforeEach, describe, expect, it } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useTabStore } from './tabStore'
import { usePanelStore } from './panelStore'
import { useSessionStore } from './sessionStore'

describe('broadcast band', () => {
  beforeEach(() => setActivePinia(createPinia()))

  function terminal(name: string, sid: string, status: 'connected' | 'connecting' | null, type = 'ssh') {
    const panels = usePanelStore(), tabs = useTabStore(), sess = useSessionStore()
    const p = panels.createPanel({ host: '10.0.0.1', name, type } as never, type as never)
    p.sessionId = sid
    if (status) { sess.initSession(sid); sess.updateStatus(sid, status) }
    tabs.createTab('terminal', name, p.id)
    return p.id
  }

  it('counts the connected sessions that receive the input', () => {
    const tabs = useTabStore()
    const a = terminal('A', 's1', 'connected')
    const b = terminal('B', 's2', 'connected')
    const c = terminal('C', 's3', 'connecting')
    tabs.toggleBroadcastPanel(a)
    tabs.toggleBroadcastPanel(b)
    tabs.toggleBroadcastPanel(c)
    expect(tabs.broadcastSessionCount).toBe(2)
    useSessionStore().updateStatus('s3', 'connected')
    expect(tabs.broadcastSessionCount).toBe(3)
  })

  it('"Stop broadcasting" turns it off everywhere', () => {
    const tabs = useTabStore()
    const a = terminal('A', 's1', 'connected')
    const b = terminal('B', 's2', 'connected')
    tabs.toggleBroadcastPanel(a)
    tabs.toggleBroadcastPanel(b)
    tabs.disableAllBroadcast()
    expect(tabs.getAllBroadcastPanelIds()).toEqual([])
    expect(tabs.broadcastSessionCount).toBe(0)
    expect(tabs.isPanelBroadcasting(a)).toBe(false)
  })
})

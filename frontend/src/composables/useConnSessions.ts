// Session state of each saved connection (item 49), for the sidebar rows and
// the Sites cards. See connectionSessions() in utils/sessionPicker.ts.
import { computed } from 'vue'
import { useTabStore } from '../stores/tabStore'
import { usePanelStore } from '../stores/panelStore'
import { useSessionStore } from '../stores/sessionStore'
import { connectionSessions, sessionStateKey, type ConnSessionInfo } from '../utils/sessionPicker'
import { useI18n } from '../i18n'

export function useConnSessions() {
  const tabStore = useTabStore()
  const panelStore = usePanelStore()
  const sessionStore = useSessionStore()
  const { t } = useI18n()

  const sessions = computed(() => connectionSessions({
    tabs: tabStore.tabs,
    getPanel: (id) => panelStore.getPanel(id),
    sessionStatus: (sid) => sessionStore.getStatus(sid),
  }))

  const sessionOf = (connId: string): ConnSessionInfo | undefined => sessions.value.get(connId)

  // Bring the open session forward instead of opening a duplicate.
  // Returns false when the connection has none.
  function focusSession(connId: string): boolean {
    const info = sessions.value.get(connId)
    if (!info) return false
    tabStore.setActiveTab(info.focus.tabId)
    if (info.focus.workspace) tabStore.setActivePanel(info.focus.tabId, info.focus.panelId)
    return true
  }

  // "PE01-REI, 10.20.2.1, sessão caída, 2 sessões" for screen readers.
  function rowLabel(name: string, detail: string, connId: string): string {
    const parts = [name]
    if (detail) parts.push(detail)
    const info = sessions.value.get(connId)
    if (info) {
      parts.push(t(sessionStateKey(info.state)))
      if (info.count > 1) parts.push(t('sessionState.count', { count: info.count }))
    }
    return parts.join(', ')
  }

  return { sessions, sessionOf, focusSession, rowLabel }
}

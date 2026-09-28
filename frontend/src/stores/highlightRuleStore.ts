import { defineStore } from 'pinia'
import { ref, watch } from 'vue'
import { Events } from '@wailsio/runtime'
import { LoadHighlightRules, SaveHighlightRules } from '../../bindings/github.com/ys-ll/uniterm/app'
import type { HighlightRuleSet } from '../composables/userHighlightRules'
import { setHighlightRuleSetsProvider, refreshAllOverlayHighlighters } from '../composables/overlayHighlight'
import { useConnectionStore } from './connectionStore'

interface HighlightRulesData {
  version: number
  sets: HighlightRuleSet[]
}

// Module-level un-subscriber for the cross-window store:highlightRules:changed listener.
let unsubHighlightRulesChanged: (() => void) | null = null

// User highlight rule sets (highlightRules.json, cloud-synced). The terminal
// highlighter reads them through a provider, so edits apply to open
// terminals without re-attaching.
export const useHighlightRuleStore = defineStore('highlightRules', () => {
  const sets = ref<HighlightRuleSet[]>([])
  const loaded = ref(false)

  setHighlightRuleSetsProvider(() => sets.value)

  async function load() {
    try {
      const data = (await LoadHighlightRules()) as unknown as HighlightRulesData
      sets.value = data?.sets ?? []
    } catch (e) {
      console.error('Failed to load highlight rules:', e)
      sets.value = []
    }
    loaded.value = true
  }

  async function save() {
    await SaveHighlightRules({
      version: 1,
      sets: JSON.parse(JSON.stringify(sets.value)),
    } as any)
  }

  // Backend pushes (another window saved, or a sync pull replaced the file).
  unsubHighlightRulesChanged?.()
  unsubHighlightRulesChanged = Events.On('store:highlightRules:changed', (ev) => {
    const data = ev.data as HighlightRulesData | undefined
    if (data?.sets) sets.value = data.sets
  })

  // Repaint open terminals right away instead of on their next output.
  watch(sets, () => refreshAllOverlayHighlighters(), { deep: true })
  // …and when a connection's per-session set picks change.
  const connectionStore = useConnectionStore()
  watch(
    () => connectionStore.connections.map(c => `${c.id}:${(c.highlightSets ?? []).join(',')}`).join('|'),
    () => refreshAllOverlayHighlighters(),
  )

  function dispose() {
    unsubHighlightRulesChanged?.()
    unsubHighlightRulesChanged = null
  }

  return { sets, loaded, load, save, dispose }
})

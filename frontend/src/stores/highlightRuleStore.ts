import { defineStore } from 'pinia'
import { computed, ref, watch } from 'vue'
import { Events } from '@wailsio/runtime'
import { LoadHighlightRules, SaveHighlightRules } from '../../bindings/github.com/ys-ll/uniterm/app'
import type { HighlightRuleSet } from '../composables/userHighlightRules'
import { presetsWithState } from '../composables/highlightPresets'
import { setHighlightRuleSetsProvider, refreshAllOverlayHighlighters } from '../composables/overlayHighlight'
import { useConnectionStore } from './connectionStore'

interface HighlightRulesData {
  version: number
  sets: HighlightRuleSet[]
  presetGlobals?: Record<string, boolean>
}

// Module-level un-subscriber for the cross-window store:highlightRules:changed listener.
let unsubHighlightRulesChanged: (() => void) | null = null

// User highlight rule sets (highlightRules.json, cloud-synced). The terminal
// highlighter reads them through a provider, so edits apply to open
// terminals without re-attaching.
export const useHighlightRuleStore = defineStore('highlightRules', () => {
  const sets = ref<HighlightRuleSet[]>([])
  /** Per-preset global override (missing → the preset's default). */
  const presetGlobals = ref<Record<string, boolean>>({})
  const loaded = ref(false)

  /** Bundled presets with the user's global choice applied. */
  const presets = computed(() => presetsWithState(presetGlobals.value))

  // User sets come first so their rules win overlaps with the presets.
  setHighlightRuleSetsProvider(() => [...sets.value, ...presets.value])

  async function load() {
    try {
      const data = (await LoadHighlightRules()) as unknown as HighlightRulesData
      sets.value = data?.sets ?? []
      presetGlobals.value = data?.presetGlobals ?? {}
    } catch (e) {
      console.error('Failed to load highlight rules:', e)
      sets.value = []
      presetGlobals.value = {}
    }
    loaded.value = true
  }

  async function save() {
    await SaveHighlightRules({
      version: 1,
      sets: JSON.parse(JSON.stringify(sets.value)),
      presetGlobals: { ...presetGlobals.value },
    } as any)
  }

  // Backend pushes (another window saved, or a sync pull replaced the file).
  unsubHighlightRulesChanged?.()
  unsubHighlightRulesChanged = Events.On('store:highlightRules:changed', (ev) => {
    const data = ev.data as HighlightRulesData | undefined
    if (data?.sets) sets.value = data.sets
    presetGlobals.value = data?.presetGlobals ?? {}
  })

  // Repaint open terminals right away instead of on their next output.
  watch([sets, presetGlobals], () => refreshAllOverlayHighlighters(), { deep: true })
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

  return { sets, presetGlobals, presets, loaded, load, save, dispose }
})

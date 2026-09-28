import { defineStore } from 'pinia'
import { computed, ref, toRaw, watch } from 'vue'
import { Events } from '@wailsio/runtime'
import { LoadHighlightRules, SaveHighlightRules } from '../../bindings/github.com/ys-ll/uniterm/app'
import { compileRules, type UserHighlightRule } from '../composables/userHighlightRules'
import { buildDefaultRules } from '../composables/highlightDefaults'
import { setHighlightRulesProvider, refreshAllOverlayHighlighters } from '../composables/overlayHighlight'
import { t } from '../i18n'

// On-disk shape (highlightRules.json, cloud-synced). Version 2 is a single
// ordered rule list; version 1 had named sets (global or per connection).
interface LegacySet {
  name?: string
  global?: boolean
  rules?: UserHighlightRule[]
}
interface HighlightRulesData {
  version: number
  rules?: UserHighlightRule[]
  sets?: LegacySet[]
}

export const HIGHLIGHT_RULES_VERSION = 2

/** Default list with group names in the current UI language. */
export function defaultRules(): UserHighlightRule[] {
  return buildDefaultRules(slug => t(`hl.group.${slug}`))
}

/** Upgrade a v1 file: its sets' rules go first (they had priority over the
 * built-ins), labeled with the set name; rules of per-connection sets are
 * kept but disabled, since there is no per-connection scope anymore. Then
 * the defaults follow. */
export function migrateLegacy(data: HighlightRulesData): UserHighlightRule[] {
  const migrated: UserHighlightRule[] = []
  for (const set of data.sets ?? []) {
    for (const r of set.rules ?? []) {
      migrated.push({
        ...r,
        group: set.name || r.group,
        enabled: set.global ? r.enabled !== false : false,
      })
    }
  }
  return [...migrated, ...defaultRules()]
}

// Module-level un-subscriber for the cross-window store:highlightRules:changed listener.
let unsubHighlightRulesChanged: (() => void) | null = null

// The single highlight rule list. Everything that colors terminal text lives
// here; the terminal highlighter reads the compiled list through a provider.
export const useHighlightRuleStore = defineStore('highlightRules', () => {
  const rules = ref<UserHighlightRule[]>([])
  const loaded = ref(false)

  // Recompiled only when the list changes, so the provider hands out a
  // stable array instance between edits.
  const compiled = computed(() => compileRules(JSON.parse(JSON.stringify(toRaw(rules.value)))))
  setHighlightRulesProvider(() => (loaded.value ? compiled.value : []))

  async function save() {
    await SaveHighlightRules({
      version: HIGHLIGHT_RULES_VERSION,
      rules: JSON.parse(JSON.stringify(rules.value)),
    } as any)
  }

  function adopt(data: HighlightRulesData | null | undefined): boolean {
    if (data && Array.isArray(data.rules) && (data.version ?? 0) >= HIGHLIGHT_RULES_VERSION) {
      rules.value = data.rules
      return false
    }
    // Missing file (first run) or a v1 file: seed and persist.
    rules.value = data?.sets?.length ? migrateLegacy(data) : defaultRules()
    return true
  }

  async function load() {
    let needsSave = false
    try {
      needsSave = adopt((await LoadHighlightRules()) as unknown as HighlightRulesData)
    } catch (e) {
      console.error('Failed to load highlight rules:', e)
      rules.value = defaultRules()
    }
    loaded.value = true
    if (needsSave) {
      try {
        await save()
      } catch (e) {
        console.error('Failed to seed highlight rules:', e)
      }
    }
  }

  async function replace(next: UserHighlightRule[]) {
    rules.value = next
    await save()
  }

  async function restoreDefaults() {
    await replace(defaultRules())
  }

  // Backend pushes (another window saved, or a sync pull replaced the file).
  unsubHighlightRulesChanged?.()
  unsubHighlightRulesChanged = Events.On('store:highlightRules:changed', (ev) => {
    const data = ev.data as HighlightRulesData | undefined
    if (data && Array.isArray(data.rules)) rules.value = data.rules
  })

  // Repaint open terminals right away instead of on their next output.
  watch(compiled, () => refreshAllOverlayHighlighters())

  function dispose() {
    unsubHighlightRulesChanged?.()
    unsubHighlightRulesChanged = null
  }

  return { rules, loaded, load, save, replace, restoreDefaults, dispose }
})

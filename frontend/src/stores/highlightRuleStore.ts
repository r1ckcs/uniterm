import { defineStore } from 'pinia'
import { computed, ref, toRaw, watch } from 'vue'
import { Events } from '@wailsio/runtime'
import { LoadHighlightRules, SaveHighlightRules } from '../../bindings/github.com/ys-ll/uniterm/app'
import { compileRules, type UserHighlightRule } from '../composables/userHighlightRules'
import { buildDefaultRules, DEFAULTS_VERSION } from '../composables/highlightDefaults'
import { baselineOf, mergeDefaults } from '../composables/highlightMerge'
import { setHighlightRulesProvider, refreshAllOverlayHighlighters } from '../composables/overlayHighlight'
import { ElMessage } from 'element-plus'
import { t } from '../i18n'

// On-disk shape (highlightRules.json, cloud-synced). Version 2 is a single
// ordered rule list; version 1 had named sets (global or per connection).
interface LegacySet {
  name?: string
  global?: boolean
  rules?: UserHighlightRule[]
}
export interface HighlightRulesData {
  version: number
  rules?: UserHighlightRule[]
  sets?: LegacySet[]
  /** DEFAULTS_VERSION the file was last merged with (missing: older file). */
  defaultsVersion?: number
  /** Default rule id → fingerprint of the rule last handed to the user. */
  defaults?: Record<string, string>
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
export function migrateLegacy(data: HighlightRulesData, defaults: UserHighlightRule[] = defaultRules()): UserHighlightRule[] {
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
  return [...migrated, ...defaults]
}

export interface AdoptResult {
  rules: UserHighlightRule[]
  baseline: Record<string, string>
  /** Default rules newly inserted into an existing list. */
  added: number
  /** The file must be rewritten (seeded, migrated or merged). */
  needsSave: boolean
}

/** Turn the loaded file into the in-memory list: seed a missing file,
 * migrate a v1 file, and merge a v2 file written against an older default
 * list. A file from a newer default list (another machine on a newer build,
 * via sync) is left alone, so an older build never undoes its additions. */
export function adoptHighlightRules(data: HighlightRulesData | null | undefined, defaults: UserHighlightRule[]): AdoptResult {
  if (data && Array.isArray(data.rules) && (data.version ?? 0) >= HIGHLIGHT_RULES_VERSION) {
    const fileVersion = data.defaultsVersion ?? 0
    if (fileVersion >= DEFAULTS_VERSION) {
      return { rules: data.rules, baseline: data.defaults ?? {}, added: 0, needsSave: false }
    }
    const merged = mergeDefaults(data.rules, fileVersion > 0 ? data.defaults ?? {} : undefined, defaults)
    return { rules: merged.rules, baseline: merged.baseline, added: merged.added, needsSave: true }
  }
  // Missing file (first run) or a v1 file: seed and persist.
  const rules = data?.sets?.length ? migrateLegacy(data, defaults) : defaults
  return { rules, baseline: baselineOf(defaults), added: 0, needsSave: true }
}

// Module-level un-subscriber for the cross-window store:highlightRules:changed listener.
let unsubHighlightRulesChanged: (() => void) | null = null

// The single highlight rule list. Everything that colors terminal text lives
// here; the terminal highlighter reads the compiled list through a provider.
export const useHighlightRuleStore = defineStore('highlightRules', () => {
  const rules = ref<UserHighlightRule[]>([])
  const loaded = ref(false)
  // Saved with the list so the next load can tell untouched defaults from
  // the user's edits (see composables/highlightMerge.ts).
  const baseline = ref<Record<string, string>>({})

  // Recompiled only when the list changes, so the provider hands out a
  // stable array instance between edits.
  const compiled = computed(() => compileRules(JSON.parse(JSON.stringify(toRaw(rules.value)))))
  setHighlightRulesProvider(() => (loaded.value ? compiled.value : []))

  async function save() {
    await SaveHighlightRules({
      version: HIGHLIGHT_RULES_VERSION,
      rules: JSON.parse(JSON.stringify(rules.value)),
      defaultsVersion: DEFAULTS_VERSION,
      defaults: { ...baseline.value },
    } as any)
  }

  function adopt(data: HighlightRulesData | null | undefined): boolean {
    const result = adoptHighlightRules(data, defaultRules())
    rules.value = result.rules
    baseline.value = result.baseline
    if (result.added > 0) {
      ElMessage({ type: 'info', message: t('hl.defaultsAdded', { count: result.added }), showClose: true })
    }
    return result.needsSave
  }

  async function load() {
    let needsSave = false
    try {
      needsSave = adopt((await LoadHighlightRules()) as unknown as HighlightRulesData)
    } catch (e) {
      console.error('Failed to load highlight rules:', e)
      rules.value = defaultRules()
      baseline.value = baselineOf(rules.value)
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
    const next = defaultRules()
    baseline.value = baselineOf(next)
    await replace(next)
  }

  // Backend pushes (another window saved, or a sync pull replaced the file).
  unsubHighlightRulesChanged?.()
  unsubHighlightRulesChanged = Events.On('store:highlightRules:changed', (ev) => {
    const data = ev.data as HighlightRulesData | undefined
    if (data && Array.isArray(data.rules)) {
      rules.value = data.rules
      if (data.defaults) baseline.value = data.defaults
    }
  })

  // Repaint open terminals right away instead of on their next output.
  watch(compiled, () => refreshAllOverlayHighlighters())

  function dispose() {
    unsubHighlightRulesChanged?.()
    unsubHighlightRulesChanged = null
  }

  return { rules, loaded, load, save, replace, restoreDefaults, dispose }
})

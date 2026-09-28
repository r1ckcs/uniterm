<template>
  <div class="hl-manager">
    <div class="hl-toolbar">
      <el-button type="primary" @click="openEditor(null)">{{ t('hl.newSet') }}</el-button>
      <el-button @click="importSets">{{ t('hl.import') }}</el-button>
      <el-button :disabled="store.sets.length === 0" @click="exportSets">{{ t('hl.export') }}</el-button>
    </div>

    <el-table :data="rows" size="small" style="margin-top: 0.75rem">
      <el-table-column :label="t('hl.setName')">
        <template #default="{ row }">
          <span>{{ row.name }}</span>
          <el-tag v-if="row.builtin" size="small" type="info" class="hl-tag">{{ t('hl.preset') }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column :label="t('hl.rules')" :width="uiPx(80)">
        <template #default="{ row }">{{ row.rules.length }}</template>
      </el-table-column>
      <el-table-column :label="t('hl.global')" :width="uiPx(90)">
        <template #default="{ row }">
          <el-switch :model-value="row.global" size="small" @change="(v: boolean) => setGlobal(row, v)" />
        </template>
      </el-table-column>
      <el-table-column :label="t('common.actions')" :width="uiPx(230)">
        <template #default="{ row }">
          <el-button size="small" @click="openEditor(row)">{{ row.builtin ? t('hl.view') : t('common.edit') }}</el-button>
          <el-button size="small" @click="duplicate(row)">{{ t('hl.duplicate') }}</el-button>
          <el-button v-if="!row.builtin" size="small" type="danger" @click="remove(row)">{{ t('common.delete') }}</el-button>
        </template>
      </el-table-column>
    </el-table>

    <HighlightSetDialog
      v-model:visible="editorVisible"
      :source="editing"
      @save="onSave"
      @duplicate="duplicate"
    />
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useI18n } from '../i18n'
import { useHighlightRuleStore } from '../stores/highlightRuleStore'
import { validateRule, type HighlightRuleSet, type UserHighlightRule } from '../composables/userHighlightRules'
import { PRESET_PREFIX } from '../composables/highlightPresets'
import { useConnectionStore } from '../stores/connectionStore'
import { uiPx } from '../utils/uiScale'
import HighlightSetDialog from './HighlightSetDialog.vue'

const { t } = useI18n()
const store = useHighlightRuleStore()
const connectionStore = useConnectionStore()

// User sets first (their rules win overlaps), then the bundled presets.
const rows = computed<HighlightRuleSet[]>(() => [...store.sets, ...store.presets])

const editorVisible = ref(false)
const editing = ref<HighlightRuleSet | null>(null)

function newId(prefix: string): string {
  return `${prefix}-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 7)}`
}

function openEditor(set: HighlightRuleSet | null) {
  editing.value = set
  editorVisible.value = true
}

async function persist() {
  try {
    await store.save()
  } catch (e) {
    ElMessage.error(`${t('hl.saveFailed')}: ${e}`)
    await store.load()
  }
}

async function setGlobal(row: HighlightRuleSet, v: boolean) {
  if (row.builtin) {
    store.presetGlobals = { ...store.presetGlobals, [row.id]: v }
  } else {
    const s = store.sets.find(x => x.id === row.id)
    if (s) s.global = v
  }
  await persist()
}

async function onSave(set: HighlightRuleSet) {
  const i = store.sets.findIndex(s => s.id === set.id)
  if (i >= 0) store.sets[i] = set
  else store.sets.push(set)
  await persist()
}

async function duplicate(row: HighlightRuleSet) {
  const copy: HighlightRuleSet = {
    id: newId('hls'),
    name: `${row.name} (${t('hl.copy')})`,
    global: false,
    rules: row.rules.map(r => ({ ...r, id: newId('hlr') })),
  }
  // Close first and reopen on the next tick: the dialog initializes its
  // form on the visible false→true transition.
  editorVisible.value = false
  await nextTick()
  openEditor(copy)
}

async function remove(row: HighlightRuleSet) {
  const inUse = connectionStore.connections.filter(c => c.highlightSets?.includes(row.id)).length
  try {
    await ElMessageBox.confirm(
      inUse ? t('hl.deleteConfirmInUse', { name: row.name, count: inUse }) : t('hl.deleteConfirm', { name: row.name }),
      { type: 'warning' },
    )
  } catch {
    return
  }
  store.sets = store.sets.filter(s => s.id !== row.id)
  await persist()
}

async function exportSets() {
  const payload = JSON.stringify({ format: 'uniterm-highlight', version: 1, sets: store.sets }, null, 2)
  try {
    await navigator.clipboard.writeText(payload)
    ElMessage.success(t('hl.exported'))
  } catch (e) {
    ElMessage.error(String(e))
  }
}

// Import from pasted JSON: only well-formed rules survive, ids are renewed
// so an import can never shadow a preset or collide with existing sets.
function sanitizeImported(raw: unknown): { sets: HighlightRuleSet[]; dropped: number } {
  const list = (raw as { sets?: unknown })?.sets
  if (!Array.isArray(list)) throw new Error(t('hl.importBadFormat'))
  let dropped = 0
  const sets: HighlightRuleSet[] = []
  for (const s of list.slice(0, 50)) {
    if (!s || typeof s !== 'object') continue
    const src = s as Partial<HighlightRuleSet>
    const rules: UserHighlightRule[] = []
    for (const r of Array.isArray(src.rules) ? src.rules.slice(0, 500) : []) {
      const rule: UserHighlightRule = {
        id: newId('hlr'),
        pattern: typeof r?.pattern === 'string' ? r.pattern : '',
        kind: r?.kind === 'regex' ? 'regex' : 'keyword',
        color: typeof r?.color === 'string' ? r.color.toLowerCase() : '',
        caseSensitive: !!r?.caseSensitive,
        wholeWord: !!r?.wholeWord,
        enabled: r?.enabled !== false,
      }
      if (validateRule(rule).ok) rules.push(rule)
      else dropped++
    }
    const name = typeof src.name === 'string' && src.name.trim() ? src.name.trim().slice(0, 60) : t('hl.imported')
    sets.push({ id: newId('hls'), name, global: !!src.global && !String(src.id ?? '').startsWith(PRESET_PREFIX), rules })
  }
  return { sets, dropped }
}

async function importSets() {
  let text: string
  try {
    const res = await ElMessageBox.prompt(t('hl.importPrompt'), t('hl.import'), {
      inputType: 'textarea',
      confirmButtonText: t('hl.import'),
    })
    text = res.value
  } catch {
    return
  }
  try {
    const { sets, dropped } = sanitizeImported(JSON.parse(text))
    if (sets.length === 0) throw new Error(t('hl.importBadFormat'))
    store.sets.push(...sets)
    await persist()
    ElMessage.success(t('hl.importedCount', { count: sets.length, dropped }))
  } catch (e) {
    ElMessage.error(e instanceof Error ? e.message : String(e))
  }
}
</script>

<style scoped>
.hl-toolbar { display: flex; gap: 0.5rem; }
.hl-tag { margin-left: 0.5rem; }
</style>

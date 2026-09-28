<template>
  <div class="hl-editor">
    <div class="hl-toolbar">
      <el-button type="primary" @click="addRule">{{ t('hl.addRule') }}</el-button>
      <el-select v-model="groupFilter" clearable :placeholder="t('hl.allGroups')" class="hl-filter">
        <el-option v-for="g in groups" :key="g" :label="g" :value="g" />
      </el-select>
      <el-input v-model="search" clearable :placeholder="t('hl.search')" class="hl-search" />
      <span class="hl-spacer" />
      <el-button @click="importRules">{{ t('hl.import') }}</el-button>
      <el-button @click="exportRules">{{ t('hl.export') }}</el-button>
      <el-button @click="restoreDefaults">{{ t('hl.restoreDefaults') }}</el-button>
    </div>

    <div class="field-hint">{{ t('hl.orderHint') }}</div>

    <div class="hl-rules">
      <div
        v-for="row in visibleRows"
        :key="row.rule.id"
        class="hl-rule"
        :class="{ invalid: !row.valid.ok, disabled: row.rule.enabled === false }"
      >
        <el-switch v-model="row.rule.enabled" size="small" :title="t('hl.ruleEnabled')" />
        <el-input v-model="row.rule.group" size="small" class="hl-group" :placeholder="t('hl.group')" maxlength="60" />
        <el-select v-model="row.rule.kind" size="small" class="hl-kind">
          <el-option :label="t('hl.kindKeyword')" value="keyword" />
          <el-option :label="t('hl.kindRegex')" value="regex" />
        </el-select>
        <el-input v-model="row.rule.pattern" size="small" class="hl-pattern" :placeholder="row.rule.kind === 'regex' ? 'gpon-onu_\\d+/\\d+/\\d+' : 'LOS'" />
        <span v-if="isTheme(row.rule.color)" class="hl-theme" :title="t('hl.themeColorDesc')">
          <i :style="{ background: themeColorFallback(row.rule.color.slice(6)) }" />{{ t('hl.themeColor') }}
        </span>
        <el-color-picker
          :model-value="isTheme(row.rule.color) ? themeColorFallback(row.rule.color.slice(6)) : row.rule.color"
          size="small"
          @update:model-value="(c: string | null) => { if (c) row.rule.color = c.toLowerCase() }"
        />
        <el-checkbox v-model="row.rule.caseSensitive" size="small" :title="t('hl.caseSensitive')">Aa</el-checkbox>
        <el-checkbox v-model="row.rule.wholeWord" size="small" :title="t('hl.wholeWordDesc')">{{ t('hl.wholeWord') }}</el-checkbox>
        <el-button size="small" text :disabled="filtered || row.index === 0" @click="move(row.index, -1)">↑</el-button>
        <el-button size="small" text :disabled="filtered || row.index === draft.length - 1" @click="move(row.index, 1)">↓</el-button>
        <el-button size="small" text type="danger" @click="draft.splice(row.index, 1)">✕</el-button>
        <div v-if="!row.valid.ok" class="hl-error">{{ errorText(row.valid) }}</div>
      </div>
      <div v-if="visibleRows.length === 0" class="hl-empty">{{ t('hl.noRules') }}</div>
    </div>

    <div class="hl-footer">
      <span class="field-hint">{{ t('hl.count', { total: draft.length, enabled: enabledCount }) }}</span>
      <span class="hl-spacer" />
      <el-button :disabled="!dirty" @click="discard">{{ t('hl.discard') }}</el-button>
      <el-button type="primary" :disabled="!dirty || !allValid" @click="save">{{ t('common.save') }}</el-button>
    </div>

    <div class="hl-preview-head">{{ t('hl.preview') }}</div>
    <el-input v-model="sample" type="textarea" :rows="4" :placeholder="t('hl.previewPlaceholder')" class="hl-sample" />
    <pre class="hl-preview"><template v-for="(line, li) in previewLines" :key="li"><span
      v-for="(seg, si) in line"
      :key="si"
      :style="seg.color ? { color: seg.color } : undefined"
    >{{ seg.text }}</span>{{ '\n' }}</template></pre>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useI18n } from '../i18n'
import { matchTextSpans } from '../composables/highlightRules'
import { compileRules, validateRule, type RuleValidation, type UserHighlightRule } from '../composables/userHighlightRules'
import { themeColorFallback } from '../composables/overlayHighlight'
import { useHighlightRuleStore, migrateLegacy } from '../stores/highlightRuleStore'

const { t } = useI18n()
const store = useHighlightRuleStore()

const clone = (rules: UserHighlightRule[]): UserHighlightRule[] => JSON.parse(JSON.stringify(rules))

// Edits happen on a draft; terminals only change on Save.
const draft = ref<UserHighlightRule[]>([])
watch(() => store.rules, (r) => { draft.value = clone(r) }, { immediate: true })

const dirty = computed(() => JSON.stringify(draft.value) !== JSON.stringify(store.rules))

const groupFilter = ref('')
const search = ref('')
const filtered = computed(() => !!groupFilter.value || !!search.value.trim())

const groups = computed(() => [...new Set(draft.value.map(r => r.group).filter((g): g is string => !!g))])

const isTheme = (c: string) => typeof c === 'string' && c.startsWith('theme:')

const validity = computed<RuleValidation[]>(() => draft.value.map(r => validateRule(r)))
const allValid = computed(() => validity.value.every(v => v.ok))
const enabledCount = computed(() => draft.value.filter(r => r.enabled !== false).length)

const visibleRows = computed(() => {
  const q = search.value.trim().toLowerCase()
  return draft.value
    .map((rule, index) => ({ rule, index, valid: validity.value[index] }))
    .filter(({ rule }) =>
      (!groupFilter.value || rule.group === groupFilter.value) &&
      (!q || rule.pattern.toLowerCase().includes(q) || (rule.group ?? '').toLowerCase().includes(q)),
    )
})

function newId(): string {
  return `hlr-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 7)}`
}

function addRule() {
  // New rules go first: earlier rules win overlaps.
  draft.value.unshift({
    id: newId(),
    pattern: '',
    kind: 'keyword',
    color: '#ff4d4f',
    caseSensitive: false,
    wholeWord: true,
    enabled: true,
    group: groupFilter.value || undefined,
  })
  search.value = ''
}

function move(i: number, d: number) {
  const [r] = draft.value.splice(i, 1)
  draft.value.splice(i + d, 0, r)
}

function errorText(v: RuleValidation): string {
  const msg = t(`hl.err.${v.code}`)
  return v.detail ? `${msg}: ${v.detail}` : msg
}

function discard() {
  draft.value = clone(store.rules)
}

async function save() {
  if (!allValid.value) return
  try {
    await store.replace(clone(draft.value))
    ElMessage.success(t('hl.saved'))
  } catch (e) {
    ElMessage.error(`${t('hl.saveFailed')}: ${e}`)
  }
}

async function restoreDefaults() {
  try {
    await ElMessageBox.confirm(t('hl.restoreConfirm'), t('hl.restoreDefaults'), { type: 'warning' })
  } catch {
    return
  }
  try {
    await store.restoreDefaults()
    ElMessage.success(t('hl.saved'))
  } catch (e) {
    ElMessage.error(`${t('hl.saveFailed')}: ${e}`)
  }
}

async function exportRules() {
  const payload = JSON.stringify({ format: 'uniterm-highlight', version: 2, rules: draft.value }, null, 2)
  try {
    await navigator.clipboard.writeText(payload)
    ElMessage.success(t('hl.exported'))
  } catch (e) {
    ElMessage.error(String(e))
  }
}

// Only well-formed rules survive; ids are renewed. Accepts the current export
// and the older set-based one.
function sanitizeImported(raw: unknown): { rules: UserHighlightRule[]; dropped: number } {
  const obj = raw as { rules?: unknown; sets?: unknown }
  let list: unknown[]
  if (Array.isArray(obj?.rules)) list = obj.rules
  else if (Array.isArray(obj?.sets)) list = migrateLegacy({ version: 1, sets: obj.sets as never }).filter(r => !r.id.match(/^d\d+$/))
  else throw new Error(t('hl.importBadFormat'))
  let dropped = 0
  const rules: UserHighlightRule[] = []
  for (const r of list.slice(0, 2000) as Array<Partial<UserHighlightRule>>) {
    const rule: UserHighlightRule = {
      id: newId(),
      pattern: typeof r?.pattern === 'string' ? r.pattern : '',
      kind: r?.kind === 'regex' ? 'regex' : 'keyword',
      color: typeof r?.color === 'string' ? r.color : '',
      caseSensitive: !!r?.caseSensitive,
      wholeWord: !!r?.wholeWord,
      trimLead: !!r?.trimLead,
      group: typeof r?.group === 'string' ? r.group.slice(0, 60) : undefined,
      enabled: r?.enabled !== false,
    }
    if (validateRule(rule).ok) rules.push(rule)
    else dropped++
  }
  return { rules, dropped }
}

async function importRules() {
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
    const { rules, dropped } = sanitizeImported(JSON.parse(text))
    if (rules.length === 0) throw new Error(t('hl.importBadFormat'))
    // Imported rules go first (priority) and stay unsaved until Save.
    draft.value.unshift(...rules)
    ElMessage.success(t('hl.importedCount', { count: rules.length, dropped }))
  } catch (e) {
    ElMessage.error(e instanceof Error ? e.message : String(e))
  }
}

const sample = ref([
  '1/7/3:27  enable  disable  LOS        1(GPON)',
  '1/7/3:28  enable  enable   working    1(GPON)',
  'gpon-onu_1/2/5:1   -23.768(dbm)   gpon-onu_1/2/5:2   -27.410(dbm)',
  'ether1 link down    %LINK-3-UPDOWN: Interface Gi0/1, changed state to down',
  'mac 48:A9:8A:12:34:56 vlan-id=203 error: connection failed at 2026-09-28 17:52:21',
  'ssh user@10.0.0.1 "cat /etc/hosts"',
].join('\n'))

// Preview of the draft (valid rules only) as plain text segments — never
// v-html, the sample is arbitrary pasted text.
const previewLines = computed(() => {
  const rules = compileRules(clone(draft.value))
  return sample.value.split('\n').slice(0, 50).map((line) => {
    const { spans } = matchTextSpans(line, rules)
    const segs: Array<{ text: string; color?: string }> = []
    let pos = 0
    for (const s of spans) {
      if (s.start > pos) segs.push({ text: line.slice(pos, s.start) })
      const c = s.color && s.color.startsWith('theme:') ? themeColorFallback(s.color.slice(6)) : s.color
      segs.push({ text: line.slice(s.start, s.end), color: c })
      pos = s.end
    }
    if (pos < line.length) segs.push({ text: line.slice(pos) })
    return segs
  })
})
</script>

<style scoped>
.hl-toolbar { display: flex; flex-wrap: wrap; align-items: center; gap: 0.5rem; }
.hl-filter { width: 11rem; }
.hl-search { width: 12rem; }
.hl-spacer { flex: 1; }
.field-hint { color: var(--el-text-color-secondary); font-size: 0.8125rem; display: block; margin: 0.5rem 0; }
.hl-rules { max-height: 26rem; overflow-y: auto; display: flex; flex-direction: column; gap: 0.25rem; }
.hl-rule { display: flex; flex-wrap: wrap; align-items: center; gap: 0.5rem; padding: 0.25rem 0.375rem; border-radius: 0.25rem; }
.hl-rule.disabled { opacity: 0.55; }
.hl-rule.invalid { background: var(--el-color-danger-light-9); opacity: 1; }
.hl-group { width: 8.5rem; }
.hl-kind { width: 6.5rem; }
.hl-pattern { flex: 1; min-width: 12rem; }
.hl-pattern :deep(input) { font-family: var(--el-font-family-monospace, monospace); }
.hl-theme { display: inline-flex; align-items: center; gap: 0.25rem; font-size: 0.75rem; color: var(--el-text-color-secondary); }
.hl-theme i { display: inline-block; width: 0.75rem; height: 0.75rem; border-radius: 0.125rem; }
.hl-error { flex-basis: 100%; color: var(--el-color-danger); font-size: 0.75rem; padding-left: 2.75rem; }
.hl-empty { color: var(--el-text-color-secondary); padding: 0.75rem 0; }
.hl-footer { display: flex; align-items: center; gap: 0.5rem; margin-top: 0.5rem; }
.hl-preview-head { font-weight: 600; margin: 1rem 0 0.375rem; }
.hl-sample :deep(textarea) { font-family: var(--el-font-family-monospace, monospace); font-size: 0.8125rem; }
.hl-preview {
  margin: 0.5rem 0 0; padding: 0.625rem 0.75rem; border-radius: 0.375rem;
  background: #1e1e1e; color: #d4d4d4; font-size: 0.8125rem; line-height: 1.45;
  max-height: 10rem; overflow: auto; white-space: pre;
}
</style>

<template>
  <el-dialog
    append-to-body
    :model-value="visible"
    :title="readonly ? t('hl.viewPreset') : (isNew ? t('hl.newSet') : t('hl.editSet'))"
    width="58rem"
    top="6vh"
    @update:model-value="(v: boolean) => emit('update:visible', v)"
  >
    <el-form label-width="7rem" class="hl-set-form">
      <el-form-item :label="t('hl.setName')">
        <el-input v-model="draft.name" :disabled="readonly" maxlength="60" />
      </el-form-item>
      <el-form-item :label="t('hl.global')">
        <el-switch v-model="draft.global" :disabled="readonly" />
        <span class="field-hint">{{ t('hl.globalDesc') }}</span>
      </el-form-item>
    </el-form>

    <div class="hl-rules-head">
      <span class="hl-rules-title">{{ t('hl.rules') }} ({{ draft.rules.length }})</span>
      <el-button v-if="!readonly" size="small" type="primary" @click="addRule">{{ t('hl.addRule') }}</el-button>
    </div>

    <div class="hl-rules">
      <div v-for="(rule, i) in draft.rules" :key="rule.id" class="hl-rule" :class="{ invalid: !validity[i].ok }">
        <el-switch v-model="rule.enabled" size="small" :disabled="readonly" :title="t('hl.ruleEnabled')" />
        <el-select v-model="rule.kind" size="small" class="hl-kind" :disabled="readonly">
          <el-option :label="t('hl.kindKeyword')" value="keyword" />
          <el-option :label="t('hl.kindRegex')" value="regex" />
        </el-select>
        <el-input
          v-model="rule.pattern"
          size="small"
          class="hl-pattern"
          :disabled="readonly"
          :placeholder="rule.kind === 'regex' ? 'gpon-onu_\\d+/\\d+/\\d+' : 'LOS'"
        />
        <el-color-picker v-model="rule.color" size="small" :disabled="readonly" />
        <el-checkbox v-model="rule.caseSensitive" size="small" :disabled="readonly">Aa</el-checkbox>
        <el-checkbox v-model="rule.wholeWord" size="small" :disabled="readonly" :title="t('hl.wholeWordDesc')">{{ t('hl.wholeWord') }}</el-checkbox>
        <template v-if="!readonly">
          <el-button size="small" text :disabled="i === 0" @click="move(i, -1)">↑</el-button>
          <el-button size="small" text :disabled="i === draft.rules.length - 1" @click="move(i, 1)">↓</el-button>
          <el-button size="small" text type="danger" @click="draft.rules.splice(i, 1)">✕</el-button>
        </template>
        <div v-if="!validity[i].ok" class="hl-error">{{ errorText(validity[i]) }}</div>
      </div>
      <div v-if="draft.rules.length === 0" class="hl-empty">{{ t('hl.noRules') }}</div>
    </div>
    <div class="field-hint hl-order-hint">{{ t('hl.orderHint') }}</div>

    <div class="hl-preview-head">{{ t('hl.preview') }}</div>
    <el-input v-model="sample" type="textarea" :rows="4" :placeholder="t('hl.previewPlaceholder')" class="hl-sample" />
    <pre class="hl-preview"><template v-for="(line, li) in previewLines" :key="li"><span
      v-for="(seg, si) in line"
      :key="si"
      :style="seg.color ? { color: seg.color } : undefined"
    >{{ seg.text }}</span>{{ '\n' }}</template></pre>

    <template #footer>
      <template v-if="readonly">
        <el-button @click="emit('update:visible', false)">{{ t('hl.close') }}</el-button>
        <el-button type="primary" @click="emit('duplicate', source!)">{{ t('hl.duplicateToEdit') }}</el-button>
      </template>
      <template v-else>
        <el-button @click="emit('update:visible', false)">{{ t('common.cancel') }}</el-button>
        <el-button type="primary" :disabled="!canSave" @click="save">{{ t('common.save') }}</el-button>
      </template>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { useI18n } from '../i18n'
import { matchTextSpans } from '../composables/highlightRules'
import {
  compileRuleSets,
  validateRule,
  type HighlightRuleSet,
  type RuleValidation,
  type UserHighlightRule,
} from '../composables/userHighlightRules'

const props = defineProps<{
  visible: boolean
  /** Set being edited/viewed; null creates a new one. */
  source: HighlightRuleSet | null
}>()
const emit = defineEmits<{
  (e: 'update:visible', v: boolean): void
  (e: 'save', set: HighlightRuleSet): void
  (e: 'duplicate', set: HighlightRuleSet): void
}>()
const { t } = useI18n()

const readonly = computed(() => !!props.source?.builtin)
const isNew = computed(() => !props.source)

function newId(prefix: string): string {
  return `${prefix}-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 7)}`
}

const draft = reactive<HighlightRuleSet>({ id: '', name: '', global: false, rules: [] })
const sample = ref('')

const DEFAULT_SAMPLE = [
  '1/7/3:27  enable  disable  LOS        1(GPON)',
  '1/7/3:28  enable  enable   working    1(GPON)',
  'gpon-onu_1/2/5:1   -23.768(dbm)   gpon-onu_1/2/5:2   -27.410(dbm)',
  'ether1 link down    %LINK-3-UPDOWN: Interface Gi0/1, changed state to down',
  'mac 48:A9:8A:12:34:56 vlan-id=203 error: connection failed',
].join('\n')

watch(() => props.visible, (v) => {
  if (!v) return
  const src = props.source
  draft.id = src?.id ?? newId('hls')
  draft.name = src?.name ?? ''
  draft.global = src?.global ?? false
  draft.rules = src
    ? src.rules.map(r => ({ enabled: true, caseSensitive: false, wholeWord: false, ...r }))
    : []
  if (!sample.value) sample.value = DEFAULT_SAMPLE
}, { immediate: true })

function addRule() {
  draft.rules.push({
    id: newId('hlr'),
    pattern: '',
    kind: 'keyword',
    color: '#ff4d4f',
    caseSensitive: false,
    wholeWord: true,
    enabled: true,
  })
}

function move(i: number, d: number) {
  const [r] = draft.rules.splice(i, 1)
  draft.rules.splice(i + d, 0, r)
}

// el-color-picker clears to null; normalize to lowercase #rrggbb.
function normalizedColor(c: unknown): string {
  return typeof c === 'string' ? c.toLowerCase() : ''
}

const validity = computed<RuleValidation[]>(() =>
  draft.rules.map(r => validateRule({ ...r, color: normalizedColor(r.color) } as UserHighlightRule)),
)

const canSave = computed(() =>
  draft.name.trim() !== '' && validity.value.every(v => v.ok),
)

function errorText(v: RuleValidation): string {
  const msg = t(`hl.err.${v.code}`)
  return v.detail ? `${msg}: ${v.detail}` : msg
}

// Preview with this set's rules only (valid ones), plain text segments —
// never v-html, the sample is arbitrary pasted text.
const previewLines = computed(() => {
  const rules = compileRuleSets([{ ...draft, rules: draft.rules.map(r => ({ ...r, color: normalizedColor(r.color) })) }])
  return sample.value.split('\n').slice(0, 50).map((line) => {
    const { spans } = matchTextSpans(line, rules)
    const segs: Array<{ text: string; color?: string }> = []
    let pos = 0
    for (const s of spans) {
      if (s.start > pos) segs.push({ text: line.slice(pos, s.start) })
      segs.push({ text: line.slice(s.start, s.end), color: s.color })
      pos = s.end
    }
    if (pos < line.length) segs.push({ text: line.slice(pos) })
    return segs
  })
})

function save() {
  if (!canSave.value) return
  emit('save', {
    id: draft.id,
    name: draft.name.trim(),
    global: draft.global,
    rules: draft.rules.map(r => ({ ...r, color: normalizedColor(r.color) })),
  })
  emit('update:visible', false)
}
</script>

<style scoped>
.field-hint { margin-left: 0.75rem; color: var(--el-text-color-secondary); font-size: 0.8125rem; }
.hl-rules-head { display: flex; align-items: center; justify-content: space-between; margin: 0.5rem 0; }
.hl-rules-title { font-weight: 600; }
.hl-rules { max-height: 16rem; overflow-y: auto; display: flex; flex-direction: column; gap: 0.375rem; }
.hl-rule { display: flex; flex-wrap: wrap; align-items: center; gap: 0.5rem; padding: 0.25rem 0.375rem; border-radius: 0.25rem; }
.hl-rule.invalid { background: var(--el-color-danger-light-9); }
.hl-kind { width: 7rem; }
.hl-pattern { flex: 1; min-width: 12rem; font-family: var(--el-font-family-monospace, monospace); }
.hl-error { flex-basis: 100%; color: var(--el-color-danger); font-size: 0.75rem; padding-left: 2.75rem; }
.hl-empty { color: var(--el-text-color-secondary); padding: 0.75rem 0; }
.hl-order-hint { margin: 0.375rem 0 0; display: block; margin-left: 0; }
.hl-preview-head { font-weight: 600; margin: 0.875rem 0 0.375rem; }
.hl-sample :deep(textarea) { font-family: var(--el-font-family-monospace, monospace); font-size: 0.8125rem; }
.hl-preview {
  margin: 0.5rem 0 0; padding: 0.625rem 0.75rem; border-radius: 0.375rem;
  background: #1e1e1e; color: #d4d4d4; font-size: 0.8125rem; line-height: 1.45;
  max-height: 10rem; overflow: auto; white-space: pre;
}
</style>

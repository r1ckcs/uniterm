<template>
  <el-dialog append-to-body
    :model-value="!!request"
    @update:model-value="(v: boolean) => !v && answer(false)"
    width="34rem"
    class="hostkey-changed-dialog"
    :close-on-click-modal="false"
    :close-on-press-escape="true"
    :show-close="false"
    @opened="focusSafe"
  >
    <template #header>
      <div class="hkc-title">
        <ShieldAlert class="hkc-title-icon" aria-hidden="true" />
        <span>{{ request?.connName
          ? t('hostKeyChanged.titleNamed', { name: request.connName, host: request.host })
          : t('hostKeyChanged.title', { host: request?.host || '' }) }}</span>
      </div>
    </template>

    <template v-if="request">
      <p class="hkc-alert" role="alert">{{ t('hostKeyChanged.message', { host: request.host }) }}</p>
      <div v-if="downgrade" class="hkc-downgrade" role="alert">
        <TriangleAlert class="hkc-downgrade-icon" aria-hidden="true" />
        <div>
          <p class="hkc-downgrade-title">{{ downgrade.from
            ? t('hostKeyChanged.typeChanged', { from: downgrade.from, to: downgrade.to })
            : t('hostKeyChanged.weakNew', { to: downgrade.to }) }}</p>
          <p v-if="downgrade.weak" class="hkc-downgrade-hint">{{ t('hostKey.weakKey', { bits: request.keyBits || '?' }) }}</p>
        </div>
      </div>

      <dl class="hkc-keys">
        <template v-for="(k, i) in request.oldKeys" :key="`old-${i}`">
          <dt>{{ t('hostKeyChanged.trusted') }}</dt>
          <dd>
            <span class="hkc-type">{{ keyTypeLabel(k.keyType) }}</span>
            <code class="hkc-fp hkc-fp-old">{{ k.fingerprint }}</code>
            <span class="hkc-file">{{ t(hostKeyFileLabelKey(k.file)) }}</span>
          </dd>
        </template>
        <dt>{{ t('hostKeyChanged.new') }}</dt>
        <dd>
          <span class="hkc-type">{{ keyTypeLabel(request.keyType) }}<template v-if="request.keyBits"> · {{ request.keyBits }} bits</template></span>
          <code class="hkc-fp hkc-fp-new">{{ request.fingerprint }}</code>
        </dd>
      </dl>

      <p v-if="request.blocker === 'pattern'" class="hkc-note hkc-note-block">{{ t('hostKeyChanged.pattern') }}</p>
      <div v-else-if="request.userFileKept" class="hkc-note">
        <p>{{ t('hostKeyChanged.userFileKept') }}</p>
        <template v-if="request.removeCommand">
          <p class="hkc-optional">{{ t('hostKeyChanged.removeOptional') }}</p>
          <div class="hkc-cmd">
            <code>{{ request.removeCommand }}</code>
            <el-button size="small" text @click="copyCommand">{{ copied ? t('hostKeyChanged.copied') : t('hostKeyChanged.copy') }}</el-button>
          </div>
        </template>
      </div>
    </template>

    <template #footer>
      <template v-if="request && canAnswerHostKeyChanged(request)">
        <el-button ref="safeRef" type="primary" class="hkc-cancel" @click="answer(false)">{{ t('hostKeyChanged.cancel') }}</el-button>
        <el-button class="hkc-replace" @click="answer(true)">{{ t('hostKeyChanged.replace') }}</el-button>
      </template>
      <el-button v-else ref="safeRef" type="primary" @click="answer(false)">{{ t('hostKeyChanged.close') }}</el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
// Red warning for a changed SSH host key (CONTRATO-chave-trocada v1.1).
// The safe way out carries the weight: Cancel is the filled button and holds
// the focus, so Enter and Esc cancel; "Replace" is a red outline that takes a
// click (or Tab to it). The dial goes on by itself after "Replace".
import { computed, nextTick, ref, watch } from 'vue'
import { ShieldAlert, TriangleAlert } from '@lucide/vue'
import type { HostKeyChangedRequest } from '../utils/hostKeyChanged'
import { canAnswerHostKeyChanged, hostKeyFileLabelKey, keyDowngrade, keyTypeLabel } from '../utils/hostKeyChanged'
import { writeClipboard } from '../composables/useClipboardWrite'
import { useI18n } from '../i18n'

const props = defineProps<{ request: HostKeyChangedRequest | null }>()
const emit = defineEmits<{ (e: 'resolve', replace: boolean): void }>()
const { t } = useI18n()

const safeRef = ref<{ $el?: HTMLElement } | null>(null)
const copied = ref(false)
const downgrade = computed(() => (props.request ? keyDowngrade(props.request) : null))
let answered = false

// The next queued request reuses the open dialog: focus Cancel again.
watch(() => props.request, (req, prev) => {
  answered = false
  copied.value = false
  if (req && prev) nextTick(focusSafe)
})

function focusSafe() {
  safeRef.value?.$el?.focus()
}

function answer(replace: boolean) {
  if (answered || !props.request) return
  answered = true
  emit('resolve', replace && canAnswerHostKeyChanged(props.request))
}

async function copyCommand() {
  if (!props.request?.removeCommand) return
  await writeClipboard(props.request.removeCommand)
  copied.value = true
}
</script>

<style scoped>
.hkc-title {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  color: var(--error);
  font-size: 1rem;
  font-weight: 800;
}
.hkc-title-icon {
  width: 1.25rem;
  height: 1.25rem;
  flex-shrink: 0;
}
.hkc-alert {
  margin: 0 0 0.75rem;
  padding: 0.625rem 0.75rem;
  border: 1px solid var(--alert-border);
  border-radius: var(--radius-row);
  background: var(--alert-card);
  color: var(--alert-text);
  font-size: 0.8125rem;
  font-weight: 600;
  line-height: 1.5;
}
.hkc-downgrade {
  display: flex;
  align-items: flex-start;
  gap: 0.5rem;
  margin: 0 0 0.75rem;
  padding: 0.5rem 0.75rem;
  border: 1px solid var(--alert-border);
  border-left: 4px solid var(--error);
  border-radius: var(--radius-row);
  background: var(--alert-card);
  color: var(--alert-text);
}
.hkc-downgrade-icon {
  width: 1.125rem;
  height: 1.125rem;
  flex-shrink: 0;
  margin-top: 0.0625rem;
}
.hkc-downgrade p {
  margin: 0;
}
.hkc-downgrade-title {
  font-size: 0.875rem;
  font-weight: 800;
}
.hkc-downgrade-hint {
  margin-top: 0.125rem !important;
  font-size: 0.75rem;
  font-weight: 600;
}
.hkc-keys {
  display: grid;
  grid-template-columns: auto 1fr;
  gap: 0.375rem 0.75rem;
  margin: 0 0 0.75rem;
  font-size: 0.75rem;
}
.hkc-keys dt {
  color: var(--text-secondary);
  font-weight: 700;
  padding-top: 0.125rem;
}
.hkc-keys dd {
  display: flex;
  flex-wrap: wrap;
  align-items: baseline;
  gap: 0.25rem 0.5rem;
  margin: 0;
  min-width: 0;
}
.hkc-type {
  color: var(--text-secondary);
}
.hkc-fp {
  flex-basis: 100%;
  font-family: var(--font-mono);
  font-size: 0.75rem;
  color: var(--text-primary);
  word-break: break-all;
}
.hkc-fp-new {
  font-weight: 700;
}
.hkc-file {
  color: var(--text-muted);
}
.hkc-note {
  margin: 0;
  padding: 0.5rem 0.75rem;
  border-radius: var(--radius-row);
  background: var(--bg-sunken);
  color: var(--text-secondary);
  font-size: 0.75rem;
  line-height: 1.5;
}
.hkc-note p {
  margin: 0;
}
.hkc-note-block {
  color: var(--text-primary);
}
.hkc-optional {
  margin-top: 0.375rem !important;
}
.hkc-cmd {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  margin-top: 0.25rem;
}
.hkc-cmd code {
  flex: 1;
  min-width: 0;
  padding: 0.25rem 0.5rem;
  border-radius: var(--radius-badge);
  background: var(--bg-input);
  color: var(--text-primary);
  font-family: var(--font-mono);
  font-size: 0.75rem;
  word-break: break-all;
  user-select: all;
}
/* Outline, not filled: replacing is the dangerous way out. The fill comes
   only on hover/focus, with --danger-solid. */
.hkc-replace {
  --el-button-bg-color: transparent;
  --el-button-border-color: var(--error);
  --el-button-text-color: var(--error);
  --el-button-hover-bg-color: var(--danger-solid);
  --el-button-hover-border-color: var(--danger-solid);
  --el-button-hover-text-color: var(--on-danger);
  --el-button-active-bg-color: var(--danger-solid-hover);
  --el-button-active-border-color: var(--danger-solid-hover);
  --el-button-active-text-color: var(--on-danger);
  font-weight: 700;
}
.hkc-cancel {
  font-weight: 700;
}
</style>

<template>
  <el-dialog append-to-body
    :model-value="!!request"
    @update:model-value="(v: boolean) => !v && answerBroadcastPaste(false)"
    :title="request ? t('broadcastPaste.title', { lines: request.lineCount, count: request.sessions.length }) : ''"
    width="34rem"
    :close-on-click-modal="false"
    @opened="focusSafe"
  >
    <template v-if="request">
      <p class="bp-label">{{ t('broadcastPaste.sessions', { count: request.sessions.length }) }}</p>
      <ul class="bp-sessions">
        <li v-for="(s, i) in request.sessions" :key="i">
          <span class="bp-name">{{ s.name }}</span>
          <span v-if="s.host && s.host !== s.name" class="bp-host"> · {{ s.host }}</span>
        </li>
      </ul>
      <p class="bp-label">{{ t('broadcastPaste.preview') }}</p>
      <pre class="bp-preview">{{ request.preview.join('\n') }}</pre>
      <p v-if="request.more > 0" class="bp-more">{{ t('broadcastPaste.more', { count: request.more }) }}</p>
    </template>
    <template #footer>
      <el-button ref="safeRef" @click="answerBroadcastPaste(false)">{{ t('conn.deleteGroupCancel') }}</el-button>
      <el-button type="danger" class="danger-solid-btn" @click="answerBroadcastPaste(true)">
        {{ request ? t('broadcastPaste.confirm', { count: request.sessions.length }) : '' }}
      </el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
// Asks before a multi-line paste goes to every broadcast session. Cancel
// holds the focus, so Enter and Esc never paste; only a click (or Tab) on
// the red button does.
import { computed, nextTick, ref, watch } from 'vue'
import { useI18n } from '../i18n'
import { answerBroadcastPaste, broadcastPasteQueue } from '../composables/useBroadcastPasteConfirm'

const { t } = useI18n()
const request = computed(() => broadcastPasteQueue.value[0] ?? null)
const safeRef = ref<{ $el?: HTMLElement } | null>(null)

watch(request, (req, prev) => { if (req && prev) nextTick(focusSafe) })

function focusSafe() {
  safeRef.value?.$el?.focus()
}
</script>

<style scoped>
.bp-label {
  margin: 0 0 0.25rem;
  color: var(--text-secondary);
  font-size: 0.8125rem;
}
.bp-sessions {
  max-height: 8rem;
  margin: 0 0 0.75rem;
  padding: 0 0 0 1.25rem;
  overflow-y: auto;
}
.bp-name {
  font-weight: 700;
}
.bp-host {
  color: var(--text-secondary);
  font-family: var(--font-mono);
}
.bp-preview {
  max-height: 10rem;
  margin: 0;
  padding: 0.5rem 0.625rem;
  overflow: auto;
  border-radius: var(--radius-badge);
  background: var(--bg-input);
  color: var(--text-primary);
  font-family: var(--font-mono);
  font-size: 0.75rem;
  line-height: 1.45;
  white-space: pre;
}
.bp-more {
  margin: 0.25rem 0 0;
  color: var(--text-secondary);
  font-size: 0.75rem;
}
</style>

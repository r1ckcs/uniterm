<template>
  <el-dialog append-to-body
    :model-value="!!request"
    @update:model-value="(v: boolean) => !v && answerDeleteConfirm(false)"
    :title="request?.title"
    width="28.125rem"
    :close-on-click-modal="false"
    @opened="focusSafe"
  >
    <p class="dc-message">{{ request?.message }}</p>
    <template #footer>
      <el-button ref="safeRef" @click="answerDeleteConfirm(false)">{{ t('conn.deleteGroupCancel') }}</el-button>
      <el-button type="danger" class="danger-solid-btn" @click="answerDeleteConfirm(true)">{{ request?.confirmText }}</el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
// Renders the head of deleteConfirmQueue. Cancel holds the focus.
import { computed, nextTick, ref, watch } from 'vue'
import { useI18n } from '../i18n'
import { answerDeleteConfirm, deleteConfirmQueue } from '../composables/useDeleteConfirm'

const { t } = useI18n()
const request = computed(() => deleteConfirmQueue.value[0] ?? null)
const safeRef = ref<{ $el?: HTMLElement } | null>(null)

// The next queued request reuses the open dialog: focus Cancel again.
watch(request, (req, prev) => { if (req && prev) nextTick(focusSafe) })

function focusSafe() {
  safeRef.value?.$el?.focus()
}
</script>

<style scoped>
.dc-message {
  margin: 0;
  overflow-wrap: anywhere;
}
</style>

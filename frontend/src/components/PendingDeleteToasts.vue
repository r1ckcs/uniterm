<template>
  <section class="pdt" role="status" aria-live="polite" :aria-label="t('conn.undoRegion')">
    <div v-for="p in connectionStore.pendingDeletes" :key="p.id" class="pdt-item">
      <span class="pdt-text">{{ deletedNotice(t, p) }}</span>
      <button type="button" class="pdt-undo" @click="connectionStore.undoDelete(p.id)">{{ t('conn.undo') }}</button>
    </div>
  </section>
</template>

<script setup lang="ts">
// Undo notices for deletions still inside their window (DELETE_UNDO_MS).
// Each one leaves on its own when its deletion commits or is undone.
import { useI18n } from '../i18n'
import { useConnectionStore } from '../stores/connectionStore'
import { deletedNotice } from '../utils/deleteText'

const { t } = useI18n()
const connectionStore = useConnectionStore()
</script>

<style scoped>
.pdt {
  position: fixed;
  right: 1rem;
  bottom: 2.25rem;
  z-index: 3000;
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  gap: 0.5rem;
  max-width: min(26rem, calc(100vw - 2rem));
  pointer-events: none;
}
.pdt-item {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  padding: 0.5rem 0.5rem 0.5rem 0.875rem;
  border: 1px solid var(--border-hover);
  border-radius: var(--radius-card, 0.5rem);
  background: var(--bg-elevated);
  color: var(--text-primary);
  box-shadow: 0 0.25rem 1rem rgb(0 0 0 / 0.25);
  font-size: 0.8125rem;
  pointer-events: auto;
}
.pdt-text {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.pdt-undo {
  flex-shrink: 0;
  padding: 0.25rem 0.625rem;
  border: 1px solid var(--accent);
  border-radius: var(--radius-badge, 0.25rem);
  background: transparent;
  color: var(--text-primary);
  font: inherit;
  font-weight: 700;
  cursor: pointer;
}
.pdt-undo:hover {
  background: var(--bg-hover);
}
.pdt-undo:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: 2px;
}
</style>

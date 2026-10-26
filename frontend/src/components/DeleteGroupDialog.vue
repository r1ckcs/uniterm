<template>
  <el-dialog append-to-body :model-value="visible" :title="t('conn.deleteGroupTitle')" width="28.125rem"
    :close-on-click-modal="false"
    @update:model-value="(v: boolean) => emit('update:visible', v)"
    @opened="focusSafe"
  >
    <template v-if="group">
      <p class="dg-ask">{{ t('conn.deleteGroupAsk', { name: group.name }) }}</p>
      <p class="dg-has">{{ t('conn.deleteGroupHas', {
        conns: t('startTab.connectionsCount', { count: counts.conns }),
        groups: t('conn.subgroupsCount', { count: counts.groups }),
      }) }}</p>
      <p class="dg-hint">{{ t('conn.deleteGroupMoveUpHint') }}</p>
    </template>
    <template #footer>
      <el-button ref="safeRef" @click="emit('update:visible', false)">{{ t('conn.deleteGroupCancel') }}</el-button>
      <el-button @click="onConfirm('move-out')">{{ t('conn.deleteGroupMoveUp') }}</el-button>
      <el-button type="danger" class="danger-solid-btn" @click="onConfirm('delete-connections')">{{ t('conn.deleteGroupAll', { count: counts.conns }) }}</el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
// Shared "delete group" dialog: delete only the group (its connections and
// sub-groups move up one level) or the group with everything inside it. The
// counts are what the red button takes, sub-groups included. Cancel holds
// the focus, so Enter never deletes; the parent schedules the removal with
// an undo window.
import { computed, ref } from 'vue'
import { useI18n } from '../i18n'
import { useConnectionStore } from '../stores/connectionStore'
import type { ConnectionGroup } from '../types/session'

const props = defineProps<{
  visible: boolean
  group: ConnectionGroup | null
}>()

const emit = defineEmits<{
  (e: 'update:visible', v: boolean): void
  (e: 'confirm', action: 'move-out' | 'delete-connections'): void
}>()

const { t } = useI18n()
const connectionStore = useConnectionStore()
const safeRef = ref<{ $el?: HTMLElement } | null>(null)

const counts = computed(() => {
  const g = props.group
  if (!g) return { conns: 0, groups: 0 }
  const inside = new Set([g.id])
  for (let grew = true; grew;) {
    grew = false
    for (const cg of connectionStore.groups) {
      if (cg.parentId && inside.has(cg.parentId) && !inside.has(cg.id)) { inside.add(cg.id); grew = true }
    }
  }
  const conns = connectionStore.connections.filter(c => c.groupId && inside.has(c.groupId)).length
  return { conns, groups: inside.size - 1 }
})

function focusSafe() {
  safeRef.value?.$el?.focus()
}

function onConfirm(action: 'move-out' | 'delete-connections') {
  emit('update:visible', false)
  emit('confirm', action)
}
</script>

<style scoped>
.dg-ask {
  margin: 0 0 0.5rem;
  font-weight: 700;
  overflow-wrap: anywhere;
}
.dg-has,
.dg-hint {
  margin: 0 0 0.25rem;
}
.dg-hint {
  color: var(--text-secondary);
  font-size: 0.8125rem;
}
</style>

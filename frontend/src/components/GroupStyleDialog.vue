<template>
  <el-dialog append-to-body :model-value="visible" :title="t('connStyle.groupTitle', { name: group?.name || '' })" width="30rem" @update:model-value="(v: boolean) => emit('update:visible', v)">
    <ConnectionStylePicker
      v-model:color="color"
      v-model:icon="icon"
      :inherit-from="group?.parentId"
      :groups="connectionStore.groups"
      :preview-name="group?.name"
      :type-icon="Folder"
    />
    <template #footer>
      <el-button @click="emit('update:visible', false)">{{ t('conn.cancel') }}</el-button>
      <el-button type="primary" @click="onConfirm">{{ t('conn.save') }}</el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
// Color and icon of a group (CONTRATO-cor-icone): connections and subgroups
// without their own inherit them.
import { computed, ref, watch } from 'vue'
import { Folder } from '@lucide/vue'
import ConnectionStylePicker from './ConnectionStylePicker.vue'
import type { ConnectionColor, ConnectionIcon } from '../types/session'
import { useConnectionStore } from '../stores/connectionStore'
import { useI18n } from '../i18n'

const props = defineProps<{ visible: boolean; groupId: string | null }>()
const emit = defineEmits<{ (e: 'update:visible', v: boolean): void }>()

const { t } = useI18n()
const connectionStore = useConnectionStore()
const group = computed(() => connectionStore.groups.find(g => g.id === props.groupId) ?? null)
const color = ref<ConnectionColor | ''>('')
const icon = ref<ConnectionIcon | ''>('')

watch(() => props.visible, (open) => {
  if (!open) return
  color.value = (group.value?.color as ConnectionColor | undefined) || ''
  icon.value = (group.value?.icon as ConnectionIcon | undefined) || ''
})

async function onConfirm() {
  if (props.groupId) await connectionStore.setGroupStyle(props.groupId, color.value, icon.value)
  emit('update:visible', false)
}
</script>

<template>
  <Menu ref="menuRef" v-model:visible="visible">
    <!-- Saved workspace record: open / rename / delete only -->
    <template v-if="config?.type === 'workspace'">
      <MenuItem @click="onOpenSavedWorkspace">{{ t('workspace.openSaved') }}</MenuItem>
      <MenuItem @click="onRenameSavedWorkspace">{{ t('workspace.renameSaved') }}</MenuItem>
      <MenuDivider />
      <MenuItem class="danger" @click="onDeleteClick">{{ t('sidebar.delete') }}</MenuItem>
    </template>
    <template v-else>
      <!-- Connect: the「连接 X」label resolves from the connectionTypes registry -->
      <MenuItem v-if="connectMenuLabelKey" @click="onConnectClick($event)">{{ t(connectMenuLabelKey) }}</MenuItem>
      <MenuSubmenu v-if="isWorkspaceHostType(config?.type)" :label="t('sidebar.connectToWorkspace')">
        <MenuItem iconic :icon="Plus" @click="onCreateWorkspace">{{ t('workspace.newWorkspace') }}</MenuItem>
        <MenuItem v-for="w in workspaceTabs" :key="w.id" @click="onConnectToWorkspace(w.id)">{{ w.name }}</MenuItem>
      </MenuSubmenu>
      <!-- Companion views: session type is not derivable from config.type -->
      <MenuItem v-if="config?.type === 'ssh'" @click="onConnectClick($event, 'file')">{{ t(connectFileMenuKey(config)) }}</MenuItem>
      <MenuItem v-if="config?.type === 'wsl'" @click="onConnectClick($event, 'wsl-file')">{{ t('sidebar.connectWslFile') }}</MenuItem>
      <MenuItem v-if="config?.type === 'ssh'" @click="onConnectClick($event, 'monitor')">{{ t('sidebar.connectMonitor') }}</MenuItem>
      <MenuItem v-if="config && isConnected" @click="locateSession">{{ t('sidebar.locateSession') }}</MenuItem>
      <MenuItem v-if="canForgetHostKey" @click="onForgetHostKey">{{ t('hostKeyForget.menu') }}</MenuItem>
      <MenuDivider />
      <MenuItem :class="{ disabled: targets.length > 1 }" @click="targets.length === 1 && emit('edit', targets[0])">{{ t('sidebar.edit') }}</MenuItem>
      <MenuItem @click="duplicate()">{{ t('sidebar.duplicate') }}</MenuItem>
      <MenuItem v-if="config" @click="favoriteStore.toggle(config.id)">{{ isFavorite ? t('sidebar.removeFromFavorites') : t('sidebar.addToFavorites') }}</MenuItem>
      <MenuDivider />
      <MenuItem @click="close(); emit('changeGroup', targets)">{{ t('conn.moveTo') }}</MenuItem>
      <MenuItem @click="close(); emit('newGroup')">{{ t('conn.newGroupTitle') }}</MenuItem>
      <MenuDivider />
      <MenuItem class="danger" @click="onDeleteClick">{{ t('sidebar.delete') }}</MenuItem>
    </template>
  </Menu>
</template>

<script setup lang="ts">
// Shared connection context menu for the sidebar list and the start page.
// Owns all type metadata (registry labels, companion items, workspace
// submenu), favorite state and duplicate/delete flows; parents only wire the
// connect routing and the edit/group dialogs.
import { computed, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { ForgetHostKey } from '../../bindings/github.com/ys-ll/uniterm/app'
import { Plus } from '@lucide/vue'
import Menu from './Menu.vue'
import MenuItem from './MenuItem.vue'
import MenuDivider from './MenuDivider.vue'
import MenuSubmenu from './MenuSubmenu.vue'
import type { ConnectionConfig } from '../types/session'
import { CONNECTION_TYPES } from '../utils/connectionTypes'
import { connectFileMenuKey } from '../utils/fileTransferUtils'
import { useI18n } from '../i18n'
import { confirmDelete } from '../composables/useDeleteConfirm'
import { deleteConnectionsPrompt } from '../utils/deleteText'
import { useFavoriteStore } from '../stores/favoriteStore'
import { usePanelStore } from '../stores/panelStore'
import { useTabStore } from '../stores/tabStore'
import { useConnectionStore } from '../stores/connectionStore'
import { isWorkspaceHostType } from '../composables/createWorkspace'

// kind selects a companion view (file browser / monitor / WSL files) whose
// session type is not derivable from config.type.
export type ConnectKind = 'file' | 'wsl-file' | 'monitor'

const props = defineProps<{
  // The connection that was right-clicked (drives the type-specific items).
  config: ConnectionConfig | null
  // Action targets: the resolved selection, guaranteed to include `config`.
  targets: ConnectionConfig[]
  visible: boolean
}>()

const emit = defineEmits<{
  (e: 'update:visible', v: boolean): void
  (e: 'connect', targets: ConnectionConfig[], kind: ConnectKind | undefined, event?: MouseEvent): void
  (e: 'connectToWorkspace', targets: ConnectionConfig[], workspaceId: string): void
  (e: 'createWorkspace', targets: ConnectionConfig[]): void
  (e: 'edit', config: ConnectionConfig): void
  (e: 'changeGroup', targets: ConnectionConfig[]): void
  (e: 'newGroup'): void
  (e: 'delete', targets: ConnectionConfig[]): void
}>()

const { t } = useI18n()
const favoriteStore = useFavoriteStore()
const panelStore = usePanelStore()
const tabStore = useTabStore()
const connectionStore = useConnectionStore()

const menuRef = ref<InstanceType<typeof Menu> | null>(null)
defineExpose({ openAt: (x: number, y: number, data?: unknown) => menuRef.value?.openAt(x, y, data) })

const visible = computed({
  get: () => props.visible,
  set: v => emit('update:visible', v),
})

const workspaceTabs = computed(() => tabStore.tabs.filter(tab => tab.type === 'workspace'))

const connectMenuLabelKey = computed(() => {
  const type = props.config?.type
  return type ? CONNECTION_TYPES.find(i => i.type === type && i.connectMenuKey)?.connectMenuKey : undefined
})

// Connection ids that currently have an open panel/session.
const isConnected = computed(() => {
  const id = props.config?.id
  if (!id) return false
  for (const p of panelStore.panels.values()) {
    if (p.config?.id === id) return true
  }
  return false
})

const isFavorite = computed(() => (props.config ? favoriteStore.isFavorite(props.config.id) : false))

function close() {
  emit('update:visible', false)
}

function onConnectClick(event: MouseEvent, kind?: ConnectKind) {
  close()
  emit('connect', props.targets, kind, event)
}

function onConnectToWorkspace(workspaceId: string) {
  close()
  emit('connectToWorkspace', props.targets, workspaceId)
}

// 新建工作区 from the current targets (multi-selection or right-clicked row).
function onCreateWorkspace() {
  close()
  emit('createWorkspace', props.targets)
}

// Open a saved workspace record — routed through the parent's connect flow,
// which branches on type === 'workspace' to openSavedWorkspace.
function onOpenSavedWorkspace() {
  close()
  if (props.config) emit('connect', [props.config], undefined)
}

// Rename a saved workspace record; keeps any live tab linked to it in sync.
async function onRenameSavedWorkspace() {
  close()
  const c = props.config
  if (!c) return
  let name = ''
  try {
    const { value } = await ElMessageBox.prompt(
      t('workspace.savePromptTitle'),
      t('workspace.renameSaved'),
      {
        inputValue: c.name,
        confirmButtonText: t('common.confirm'),
        cancelButtonText: t('common.cancel')
      }
    )
    name = value?.trim() || ''
  } catch {
    return
  }
  if (!name || name === c.name) return
  await connectionStore.update(c.id, { name })
  const tabStore = useTabStore()
  for (const tb of tabStore.tabs) {
    if (tb.type === 'workspace' && tb.savedWorkspaceId === c.id) tabStore.renameTab(tb.id, name)
  }
}

// Switch to the existing tab/session hosting this connection.
function locateSession() {
  close()
  const id = props.config?.id
  if (!id) return
  for (const p of panelStore.panels.values()) {
    if (p.config?.id !== id) continue
    const tab = tabStore.tabs.find(tb => tb.id === p.tabId)
    if (!tab) continue
    tabStore.setActiveTab(tab.id)
    if (tab.type === 'workspace') {
      tabStore.setActivePanel(tab.id, p.id)
    }
    break
  }
}

function generateDuplicateName(name: string): string {
  const match = name.match(/^(.*)\s*\((\d+)\)$/)
  const base = match ? match[1].trim() : name
  const re = new RegExp('^' + escapeRegex(base) + '\s*\(\d+\)$')
  let maxNum = 0
  for (const c of connectionStore.connections) {
    if (c.name === base || re.test(c.name)) {
      const m = c.name.match(/\((\d+)\)$/)
      if (m) {
        maxNum = Math.max(maxNum, parseInt(m[1], 10))
      } else {
        maxNum = Math.max(maxNum, 0)
      }
    }
  }
  return `${base} (${maxNum + 1})`
}

function escapeRegex(str: string): string {
  return str.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

async function duplicate() {
  close()
  for (const c of props.targets) {
    const dup: ConnectionConfig = {
      ...c,
      id: `conn-${Date.now()}-${Math.random().toString(36).slice(2, 7)}`,
      name: generateDuplicateName(c.name)
    }
    connectionStore.add(dup)
  }
}

// "Forget host key" (CONTRATO-chave-trocada): drops the app's trusted keys of
// host:port, so the next connection asks as for a new host. The user's
// ~/.ssh/known_hosts is never edited; if it still has the host, show the
// optional command to clean it there.
const canForgetHostKey = computed(() =>
  props.targets.length === 1 && !!props.config?.host && (props.config.type === 'ssh' || props.config.type === 'mosh'))

async function onForgetHostKey() {
  close()
  const c = props.config
  if (!c?.host) return
  const host = `${c.host}${c.port && c.port !== 22 ? `:${c.port}` : ''}`
  try {
    await ElMessageBox.confirm(t('hostKeyForget.confirm', { host }), t('hostKeyForget.menu'), {
      confirmButtonText: t('hostKeyForget.confirmButton'),
      cancelButtonText: t('sftp.dialog.cancel'),
      type: 'warning',
    })
  } catch {
    return
  }
  try {
    const r = await ForgetHostKey(c.host, c.port || 22)
    if (r?.inUserFile) {
      await ElMessageBox.alert(
        r.removeCommand ? t('hostKeyForget.userFileCmd', { command: r.removeCommand }) : t('hostKeyForget.userFile'),
        t('hostKeyForget.menu'),
        { confirmButtonText: t('hostKeyChanged.close') },
      ).catch(() => {})
    } else {
      ElMessage.success(r?.removed ? t('hostKeyForget.done') : t('hostKeyForget.none'))
    }
  } catch (e) {
    ElMessage.error(String(e))
  }
}

// Confirms internally (naming what goes, Cancel focused), then hands the
// targets to the parent, which schedules the removal with an undo window.
async function onDeleteClick() {
  close()
  const ok = await confirmDelete({
    title: t('sidebar.delete'),
    message: deleteConnectionsPrompt(t, props.targets),
    confirmText: t('sidebar.delete'),
  })
  if (ok) emit('delete', props.targets)
}
</script>

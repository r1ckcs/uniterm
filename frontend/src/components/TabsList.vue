<template>
  <div class="tabs-list" ref="tabsListRef" role="tablist" :aria-label="t('tab.listLabel')" @keydown="onTablistKeydown" @wheel="onWheel" @dragover.prevent="onTabsContainerDragOver" @dragleave="onTabsDragLeave" @drop="onTabsContainerDrop">
    <template v-for="(tab, index) in tabs" :key="tab.id">
      <div
        v-if="(dragOverTabIndex === index && !dragOverInsertAfter) || (dragOverTabIndex === index - 1 && dragOverInsertAfter)"
        class="tab-drop-indicator"
      ></div>

      <TabItem
        :tab="tab"
        :shortcut-index="index + 1"
        :is-active="tab.id === activeTabId"
        :tab-stop="tab.id === tabStopId"
        :has-notification="tabStore.hasTabNotification(tab.id)"
        :progress="tabStore.getTabProgress(tab.id)"
        @activate="setActiveTab"
        @focus-tab="(id: string) => (tabFocusId = id)"
        @close="(id: string) => $emit('close-tab', id)"
        @close-batch="(ids: string[]) => $emit('close-tab-batch', ids)"
        @toggle-ai-lock="(panelId: string) => $emit('toggle-ai-lock', panelId)"
        @dragstart="(e: DragEvent, tabId: string) => $emit('tab-dragstart', e, tabId)"
        @dragover.prevent="(e: DragEvent) => onTabDragOver(e, index)"
        @dragleave="onTabDragLeave"
        @drop="(e: DragEvent) => onTabDrop(e, tab.id, index)"
      />
    </template>
    <div
      v-if="dragOverTabIndex === tabs.length - 1 && dragOverInsertAfter"
      class="tab-drop-indicator"
    ></div>
    <!-- Drop indicator at end when dragging over empty tabs area (renders
         inline after the last tab, same spot as the per-tab insert-after
         indicator — no auto margin, which used to fling the + button to the
         far right edge, #922) -->
    <div
      v-if="dragOverContainer"
      class="tab-drop-indicator"
    ></div>
    <button
      v-if="!showMore"
      class="tab-add-btn"
      :title="t('startTab.defaultName') + shortcutSuffix('newConnection')"
      @click="onAddStartTab"
    >
      <Plus :size="'0.875rem'" />
    </button>
  </div>
  <div class="tab-more" v-if="showMore">
    <!-- Every tab by name, keyboard included: the session picker in 'tabs'
         mode (item 44), with full names. -->
    <button
      type="button"
      class="tab-more-btn"
      :title="t('tab.moreHint', { count: tabs.length })"
      :aria-label="t('tab.moreHint', { count: tabs.length })"
      aria-haspopup="dialog"
      @click.stop="openTabList"
    >
      <el-icon class="tab-more-icon"><MoreHorizontal :size="'0.875rem'" /></el-icon>
    </button>
    <button
      class="tab-add-btn"
      :title="t('startTab.defaultName') + shortcutSuffix('newConnection')"
      @click="onAddStartTab"
    >
      <Plus :size="'0.875rem'" />
    </button>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, nextTick, onMounted, onUnmounted } from 'vue'
import { MoreHorizontal, Plus } from '@lucide/vue'
import { useTabStore } from '../stores/tabStore'
import { usePanelStore } from '../stores/panelStore'
import { useI18n } from '../i18n'
import { useSettingsStore } from '../stores/settingsStore'
import { formatKeyBinding } from '../composables/useKeyboardShortcuts'
import TabItem from './TabItem.vue'

const tabStore = useTabStore()
const panelStore = usePanelStore()
const settingsStore = useSettingsStore()
const { t } = useI18n()

const isMac = /Mac|iPhone|iPad/.test(navigator.userAgent)

// " (Ctrl+Shift+N)" suffix for the new-tab tooltip, '' when unset. Reactive via
// settingsStore, so the tooltip updates when the user rebinds the shortcut.
function shortcutSuffix(action: 'newConnection'): string {
  const b = settingsStore.settings.keyboard[action]
  if (!b) return ''
  const key = formatKeyBinding(b, isMac)
  return key ? ` (${key})` : ''
}

const tabs = computed(() => tabStore.tabs)
const activeTabId = computed(() => tabStore.activeTabId)

const dragOverTabIndex = ref<number | null>(null)
const dragOverInsertAfter = ref(false)
const dragOverContainer = ref(false)

const tabsListRef = ref<HTMLElement | null>(null)
const showMore = ref(false)

function updateOverflow() {
  const el = tabsListRef.value
  if (!el) return
  showMore.value = el.scrollWidth > el.clientWidth + 1
}

// Watch tab changes and window resize to update overflow state

watch(() => tabs.value.length, () => nextTick(updateOverflow))
watch(activeTabId, () => nextTick(updateOverflow))

let resizeObserver: ResizeObserver | null = null

onMounted(() => {
  resizeObserver = new ResizeObserver(updateOverflow)
  if (tabsListRef.value) {
    resizeObserver.observe(tabsListRef.value)
  }
  nextTick(updateOverflow)
  window.addEventListener('dragend', clearDragState)
})

onUnmounted(() => {
  resizeObserver?.disconnect()
  window.removeEventListener('dragend', clearDragState)
})

defineEmits<{
  'close-tab': [id: string]
  'close-tab-batch': [ids: string[]]
  'toggle-ai-lock': [panelId: string]
  'tab-dragstart': [e: DragEvent, tabId: string]
}>()

function onAddStartTab() {
  tabStore.createStartTab()
}

function onWheel(e: WheelEvent) {
  if (!tabsListRef.value) return
  tabsListRef.value.scrollLeft += e.deltaY
}

function openTabList() {
  window.dispatchEvent(new CustomEvent('app:session-picker', { detail: { mode: 'tabs' } }))
}
// A tab picked from the list (or ⌘K) may be scrolled out of the bar.
watch(activeTabId, (id) => { if (id) nextTick(() => scrollToTab(id)) })

// ── Tablist keyboard (item 45): one tab stop, ←/→/Home/End move the focus,
// Enter/Space open the focused tab (manual activation: switching a terminal
// on every arrow press would be heavy). ──
const tabFocusId = ref<string | null>(null)
const tabStopId = computed(() =>
  tabFocusId.value && tabs.value.some(t => t.id === tabFocusId.value) ? tabFocusId.value : activeTabId.value)
watch(activeTabId, () => { tabFocusId.value = null })

function onTablistKeydown(e: KeyboardEvent) {
  const target = (e.target as HTMLElement)?.closest?.('[role="tab"]') as HTMLElement | null
  if (!target || (e.target as HTMLElement).closest('input')) return
  const ids = tabs.value.map(t => t.id)
  const i = ids.indexOf(target.dataset.tabId || '')
  if (i < 0) return
  let next = -1
  if (e.key === 'ArrowRight') next = (i + 1) % ids.length
  else if (e.key === 'ArrowLeft') next = (i - 1 + ids.length) % ids.length
  else if (e.key === 'Home') next = 0
  else if (e.key === 'End') next = ids.length - 1
  if (next < 0) return
  e.preventDefault()
  tabFocusId.value = ids[next]
  nextTick(() => {
    const el = tabsListRef.value?.querySelector(`[data-tab-id="${ids[next]}"]`) as HTMLElement | null
    el?.focus()
    el?.scrollIntoView({ block: 'nearest', inline: 'nearest' })
  })
}

function setActiveTab(id: string) {
  tabStore.setActiveTab(id)
  scrollToTab(id)
}

function scrollToTab(tabId: string) {
  if (!tabsListRef.value) return
  const el = tabsListRef.value.querySelector(`[data-tab-id="${tabId}"]`) as HTMLElement | null
  if (el) {
    el.scrollIntoView({ behavior: 'smooth', block: 'nearest', inline: 'nearest' })
  }
}

function onTabDragOver(e: DragEvent, index: number) {
  // A terminal-type connection dragged over a workspace tab activates it so
  // the user can aim at a panel for the connect-and-split drop (drop on the
  // tab itself opens a new tab at that position instead).
  if (e.dataTransfer?.types.includes('application/conn-id')) {
    clearDragState()
    const tab = tabStore.tabs[index]
    if (tab.type === 'workspace') tabStore.setActiveTab(tab.id)
  }

  const hasPanel = e.dataTransfer?.types.includes('application/panel-id')
  const hasTab = e.dataTransfer?.types.includes('application/tab-id')
  const hasConnOpen = e.dataTransfer?.types.includes('application/conn-open')
  if (!hasPanel && !hasTab && !hasConnOpen) return

  if (hasConnOpen) {
    const tab = tabStore.tabs[index]
    // Keep workspace tabs revealed while aiming at their panels.
    if (tab.type === 'workspace') tabStore.setActiveTab(tab.id)
    dragOverContainer.value = false
  }

  dragOverContainer.value = false
  const el = e.currentTarget as HTMLElement
  const rect = el.getBoundingClientRect()
  dragOverTabIndex.value = index
  dragOverInsertAfter.value = e.clientX >= rect.left + rect.width / 2
  e.dataTransfer!.dropEffect = 'move'
}

function onTabDragLeave(_e: DragEvent) {
  // Reset handled by onTabDragOver of adjacent tab
}

function onTabsDragLeave(e: DragEvent) {
  const el = e.currentTarget as HTMLElement
  const relatedTarget = e.relatedTarget as HTMLElement | null
  if (!relatedTarget || !el.contains(relatedTarget)) {
    clearDragState()
  }
}

function clearDragState() {
  dragOverTabIndex.value = null
  dragOverInsertAfter.value = false
  dragOverContainer.value = false
}

function onTabsContainerDragOver(e: DragEvent) {
  const hasPanel = e.dataTransfer?.types.includes('application/panel-id')
  const hasTab = e.dataTransfer?.types.includes('application/tab-id')
  const hasConnOpen = e.dataTransfer?.types.includes('application/conn-open')
  if (!hasPanel && !hasTab && !hasConnOpen) return

  // Ignore if hovering over a tab item (handled by onTabDragOver)
  const target = e.target as HTMLElement
  if (target.closest('.tab-item') || target.closest('.workspace-tab-item')) {
    return
  }

  dragOverContainer.value = true
  dragOverTabIndex.value = null
  dragOverInsertAfter.value = false
  e.dataTransfer!.dropEffect = 'move'
}

function onTabsContainerDrop(e: DragEvent) {
  e.stopPropagation()

  // Skip if drop landed on a tab item (already handled by onTabDrop)
  const target = e.target as HTMLElement
  if (target.closest('.tab-item') || target.closest('.workspace-tab-item')) {
    clearDragState()
    return
  }

  const draggedTabId = e.dataTransfer?.getData('application/tab-id')
  const panelId = e.dataTransfer?.getData('application/panel-id')
  const sourceTabId = e.dataTransfer?.getData('application/source-tab-id')
  const connOpenId = e.dataTransfer?.getData('application/conn-open')

  clearDragState()

  // Case 0: Sidebar connection dropped on empty bar area → open at the end
  if (connOpenId) {
    window.dispatchEvent(new CustomEvent('app:open-connection-at', {
      detail: { connId: connOpenId, index: tabs.value.length }
    }))
    return
  }

  // Case 1: Tab dragged to empty area → move to end
  if (draggedTabId && !panelId) {
    const fromIdx = tabs.value.findIndex(t => t.id === draggedTabId)
    if (fromIdx === -1) return
    const toIdx = tabs.value.length - 1
    if (fromIdx !== toIdx) {
      tabStore.moveTab(fromIdx, toIdx)
    }
    return
  }

  // Case 2: Panel dropped on empty area → create terminal tab at end
  if (panelId) {
    const panel = panelStore.getPanel(panelId)
    if (!panel) return

    if (sourceTabId) {
      tabStore.removePanelFromWorkspaceTab(sourceTabId, panelId)
    }

    const tab = tabStore.createTerminalTab(panel.title, panelId)
    panelStore.movePanelToTab(panelId, tab.id)
  }
}

function onTabDrop(e: DragEvent, targetTabId: string, index: number) {
  e.stopPropagation()

  const insertAfter = dragOverInsertAfter.value
  clearDragState()

  // Sidebar connection dropped between tabs → open a new tab there.
  const connOpenId = e.dataTransfer?.getData('application/conn-open')
  if (connOpenId) {
    window.dispatchEvent(new CustomEvent('app:open-connection-at', {
      detail: { connId: connOpenId, index: index + (insertAfter ? 1 : 0) }
    }))
    return
  }

  const draggedTabId = e.dataTransfer?.getData('application/tab-id')
  const draggedPanelId = e.dataTransfer?.getData('application/panel-id')
  const sourceTabId = e.dataTransfer?.getData('application/source-tab-id')

  if (draggedTabId && !draggedPanelId) {
    if (draggedTabId === targetTabId) return
    const fromIdx = tabs.value.findIndex(t => t.id === draggedTabId)
    if (fromIdx === -1) return
    let toIdx = index + (insertAfter ? 1 : 0)
    if (toIdx > fromIdx) toIdx--
    if (fromIdx !== toIdx) {
      tabStore.moveTab(fromIdx, toIdx)
    }
    return
  }

  if (draggedPanelId) {
    const panel = panelStore.getPanel(draggedPanelId)
    if (!panel) return

    if (sourceTabId) {
      tabStore.removePanelFromWorkspaceTab(sourceTabId, draggedPanelId)
    }

    const tab = tabStore.createTerminalTab(panel.title, draggedPanelId)
    panelStore.movePanelToTab(draggedPanelId, tab.id)

    const targetIdx = index + (insertAfter ? 1 : 0)
    const currentIdx = tabs.value.findIndex(t => t.id === tab.id)
    if (currentIdx !== targetIdx) {
      tabStore.moveTab(currentIdx, targetIdx)
    }
  }
}
</script>

<style scoped>
.tabs-list {
  display: flex;
  flex: 1;
  overflow-x: auto;
  overflow-y: hidden;
  align-items: center;
  scrollbar-width: none;
  min-width: 0;
}
.tabs-list::-webkit-scrollbar {
  display: none;
}

.tab-more {
  display: flex;
  align-items: center;
  flex-shrink: 0;
  padding: 0 0.25rem;
  height: 1.75rem;
}
.tab-more-btn {
  border: none;
  background: transparent;
  padding: 0;
  font: inherit;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 1.5rem;
  height: 1.5rem;
  border-radius: var(--radius-sm);
  cursor: pointer;
  font-size: 0.875rem;
  font-weight: 600;
  color: var(--text-muted);
  letter-spacing: 1px;
  user-select: none;
  transition: all 0.15s;
  --wails-draggable: no-drag;
}
.tab-more-btn:focus-visible {
  outline: 2px solid var(--header-focus-ring);
  outline-offset: 1px;
}
.tab-more-btn:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
}

.tab-drop-indicator {
  width: 0.125rem;
  min-width: 0.125rem;
  align-self: stretch;
  background: var(--accent);
  opacity: 0.8;
  margin: 0.25rem 0;
  border-radius: 1px;
  flex-shrink: 0;
}
/* 拖到标签条空白处时的"插到末尾"指示条。不带 margin-left:auto——那会把
   自由空间全吸到自己前面，连着后面的新增 + 按钮一起甩到最右边 (#922)。
   不加 auto 时它落在最后一个标签之后、+ 按钮之前，与"拖过末尾标签右半"
   的指示条（onTabDragOver 的 insert-after 分支）位置一致。 */
.tab-add-btn {
  flex-shrink: 0;
  width: 1.75rem;
  height: 1.75rem;
  border: none;
  border-radius: 0.375rem;
  background: transparent;
  color: var(--text-secondary);
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: background 0.15s, color 0.15s;
  margin-left: 0.125rem;
  --wails-draggable: no-drag;
}
.tab-add-btn:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
}
</style>

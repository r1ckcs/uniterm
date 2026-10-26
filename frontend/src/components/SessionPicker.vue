<template>
  <Teleport to="body">
    <div v-if="visible" class="sp-scrim" @mousedown.self="close">
      <div class="sp" role="dialog" aria-modal="true" :aria-label="title">
        <div class="sp-search">
          <Search class="sp-search-icon" aria-hidden="true" />
          <input
            ref="inputRef"
            v-model="query"
            class="sp-input"
            type="text"
            role="combobox"
            aria-autocomplete="list"
            aria-expanded="true"
            :aria-controls="listId"
            :aria-activedescendant="activeItem ? optionId(activeItem) : undefined"
            :aria-label="title"
            :placeholder="placeholder"
            spellcheck="false"
            autocomplete="off"
            @keydown="onKeydown"
          />
          <kbd class="sp-kbd">Esc</kbd>
        </div>

        <ul :id="listId" ref="listRef" class="sp-list" role="listbox" :aria-label="title">
          <template v-for="group in visibleGroups" :key="group.id">
            <li class="sp-group" role="presentation">{{ group.label }}</li>
            <li
              v-for="item in group.items"
              :id="optionId(item)"
              :key="item.key"
              class="sp-item"
              :class="{ active: activeItem?.key === item.key, down: item.kind === 'session' && item.state === 'down' }"
              role="option"
              :aria-selected="activeItem?.key === item.key"
              @mousemove="setActive(item)"
              @click="pick(item)"
            >
              <span class="sp-state" :class="item.kind === 'session' ? `sp-state-${item.state}` : 'sp-state-none'" aria-hidden="true"></span>
              <span class="sp-main">
                <span class="sp-name">{{ item.name }}</span>
                <span class="sp-meta">
                  <span v-if="item.kind !== 'tab'" class="sp-proto">{{ item.protocol }}</span>
                  <span v-if="item.kind !== 'tab' && item.host" class="sp-host">{{ item.host }}</span>
                  <span v-if="item.kind === 'session' && item.workspace" class="sp-ws">{{ t('picker.inWorkspace', { name: item.workspace }) }}</span>
                </span>
              </span>
              <span v-if="item.kind === 'session'" class="sp-side">
                <span v-if="item.latencyMs !== undefined && item.state === 'connected'" class="sp-latency">↔ {{ formatLatency({ state: 'ok', rttMs: item.latencyMs }) }}</span>
                <span v-if="item.broadcasting" class="sp-badge sp-badge-broadcast"><Radio class="sp-badge-icon" aria-hidden="true" />{{ t('picker.broadcast') }}</span>
                <span class="sp-badge" :class="`sp-badge-${item.state}`">{{ t(`picker.state.${item.state}`) }}</span>
              </span>
              <span v-else-if="item.kind === 'saved'" class="sp-side sp-action">{{ t('picker.connect') }}</span>
            </li>
          </template>
          <li v-if="!flat.length" class="sp-empty" role="presentation">{{ t('picker.empty') }}</li>
        </ul>

        <div class="sp-foot" aria-hidden="true">
          <span><kbd>↑</kbd><kbd>↓</kbd> {{ t('picker.hintMove') }}</span>
          <span><kbd>Enter</kbd> {{ t('picker.hintOpen') }}</span>
          <span><kbd>Esc</kbd> {{ t('picker.hintClose') }}</span>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<script setup lang="ts">
// ⌘K / Ctrl+Shift+P session picker (item 44), also the "…" tab list.
// Open sessions come first (go to the tab), then saved connections (connect,
// as the start page does). A combobox drives a listbox: arrows move, Enter
// picks, Esc closes; the active option is announced via
// aria-activedescendant.
import { computed, nextTick, ref, watch } from 'vue'
import { Radio, Search } from '@lucide/vue'
import type { ConnectionConfig } from '../types/session'
import { useTabStore } from '../stores/tabStore'
import { usePanelStore } from '../stores/panelStore'
import { useSessionStore } from '../stores/sessionStore'
import { useConnectionStore } from '../stores/connectionStore'
import { useLatencyStore } from '../stores/latencyStore'
import { getShellLabel } from '../utils/shellLabel'
import { formatLatency } from '../utils/latency'
import {
  buildPickerGroups, filterPickerGroups, flattenPickerGroups, movePickerIndex,
  type PickerItem, type PickerMode,
} from '../utils/sessionPicker'
import { useI18n } from '../i18n'

const props = defineProps<{ visible: boolean; mode: PickerMode; shortcut?: string }>()
const emit = defineEmits<{
  (e: 'close'): void
  (e: 'connect', config: ConnectionConfig): void
}>()

const { t } = useI18n()
const tabStore = useTabStore()
const panelStore = usePanelStore()
const sessionStore = useSessionStore()
const connectionStore = useConnectionStore()
const latencyStore = useLatencyStore()

const listId = `sp-list-${Math.random().toString(36).slice(2, 8)}`
const inputRef = ref<HTMLInputElement | null>(null)
const listRef = ref<HTMLElement | null>(null)
const query = ref('')
const activeIndex = ref(-1)

const title = computed(() => (props.mode === 'tabs' ? t('picker.titleTabs') : t('picker.title')))
const placeholder = computed(() =>
  props.mode === 'tabs' ? t('picker.placeholderTabs') : t('picker.placeholder', { shortcut: props.shortcut || '' }))

const groups = computed(() => buildPickerGroups({
  tabs: tabStore.tabs,
  getPanel: (id) => panelStore.getPanel(id),
  sessionStatus: (sid) => sessionStore.getStatus(sid),
  latencyMs: (sid) => {
    const s = latencyStore.sampleOf(sid)
    return s?.state === 'ok' ? s.rttMs : undefined
  },
  isBroadcasting: (pid) => tabStore.isPanelBroadcasting(pid),
  connections: connectionStore.connections,
  shellLabel: (p) => getShellLabel(p, 'Local'),
}, props.mode))

const filtered = computed(() => filterPickerGroups(groups.value, query.value))
const flat = computed(() => flattenPickerGroups(filtered.value))
const activeItem = computed<PickerItem | undefined>(() => flat.value[activeIndex.value])

const visibleGroups = computed(() => [
  { id: 'sessions', label: t('picker.groupSessions', { count: filtered.value.sessions.length }), items: filtered.value.sessions as PickerItem[] },
  { id: 'tabs', label: t('picker.groupTabs'), items: filtered.value.tabs as PickerItem[] },
  { id: 'saved', label: t('picker.groupSaved'), items: filtered.value.saved as PickerItem[] },
].filter((g) => g.items.length))

function optionId(item: PickerItem) {
  return `${listId}-${item.key.replace(/[^\w-]/g, '_')}`
}

watch(() => props.visible, (v) => {
  if (!v) return
  query.value = ''
  // Start on the first session that is not the tab already in front.
  const firstOther = flat.value.findIndex((i) => i.kind !== 'saved' && i.tabId !== tabStore.activeTabId)
  activeIndex.value = firstOther >= 0 ? firstOther : flat.value.length ? 0 : -1
  nextTick(() => inputRef.value?.focus())
}, { immediate: true })

watch(query, () => { activeIndex.value = flat.value.length ? 0 : -1 })

watch(activeIndex, () => nextTick(() => {
  const el = activeItem.value && document.getElementById(optionId(activeItem.value))
  el?.scrollIntoView({ block: 'nearest' })
}))

function setActive(item: PickerItem) {
  const i = flat.value.findIndex((x) => x.key === item.key)
  if (i >= 0) activeIndex.value = i
}

function onKeydown(e: KeyboardEvent) {
  const n = flat.value.length
  if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
    e.preventDefault()
    activeIndex.value = movePickerIndex(activeIndex.value, e.key === 'ArrowDown' ? 1 : -1, n)
  } else if (e.key === 'PageDown' || e.key === 'PageUp') {
    e.preventDefault()
    if (n) activeIndex.value = Math.max(0, Math.min(n - 1, activeIndex.value + (e.key === 'PageDown' ? 8 : -8)))
  } else if (e.key === 'Enter') {
    e.preventDefault()
    if (activeItem.value) pick(activeItem.value)
  } else if (e.key === 'Escape') {
    e.preventDefault()
    e.stopPropagation()
    close()
  }
}

function pick(item: PickerItem) {
  close()
  if (item.kind === 'saved') {
    emit('connect', item.config)
    return
  }
  tabStore.setActiveTab(item.tabId)
  if (item.kind === 'session' && item.workspace) tabStore.setActivePanel(item.tabId, item.panelId)
}

function close() {
  emit('close')
}
</script>

<style scoped>
.sp-scrim {
  position: fixed;
  inset: 0;
  z-index: 3000;
  display: flex;
  justify-content: center;
  align-items: flex-start;
  padding: 12vh 1rem 1rem;
  background: var(--scrim);
}
.sp {
  display: flex;
  flex-direction: column;
  width: min(42rem, 100%);
  max-height: 72vh;
  overflow: hidden;
  border: 1px solid var(--border-subtle);
  border-radius: var(--radius-card);
  background: var(--bg-elevated);
  color: var(--text-primary);
}
.sp-search {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  padding: 0.625rem 0.875rem;
  border-bottom: 1px solid var(--border-subtle);
}
.sp-search-icon {
  width: 1rem;
  height: 1rem;
  flex-shrink: 0;
  color: var(--text-secondary);
}
.sp-input {
  flex: 1;
  min-width: 0;
  border: none;
  outline: none;
  background: transparent;
  color: var(--text-primary);
  font: inherit;
  font-size: 0.9375rem;
}
.sp-input::placeholder {
  color: var(--text-muted);
}
.sp-kbd,
.sp-foot kbd {
  padding: 0.0625rem 0.3125rem;
  border: 1px solid var(--border-subtle);
  border-radius: var(--radius-badge);
  background: var(--bg-sunken);
  color: var(--text-secondary);
  font-family: var(--font-ui);
  font-size: 0.6875rem;
}
.sp-list {
  flex: 1;
  min-height: 0;
  margin: 0;
  padding: 0.25rem 0.375rem 0.375rem;
  overflow-y: auto;
  list-style: none;
}
.sp-group {
  padding: 0.625rem 0.5rem 0.25rem;
  color: var(--text-secondary);
  font-size: 0.6875rem;
  font-weight: 800;
  letter-spacing: 0.04em;
  text-transform: uppercase;
}
.sp-item {
  display: flex;
  align-items: center;
  gap: 0.625rem;
  padding: 0.4375rem 0.5rem;
  border-radius: var(--radius-row);
  cursor: pointer;
}
.sp-item.active {
  background: var(--bg-hover);
  box-shadow: inset 3px 0 0 var(--accent);
}
.sp-state {
  width: 0.5rem;
  height: 0.5rem;
  flex-shrink: 0;
  border-radius: 50%;
}
.sp-state-connected {
  background: var(--success);
}
.sp-state-connecting {
  border: 2px solid var(--warning);
}
.sp-state-down {
  background: var(--error);
}
.sp-state-none {
  border: 1px solid var(--border-hover);
}
.sp-main {
  display: flex;
  flex-direction: column;
  flex: 1;
  min-width: 0;
}
.sp-name {
  overflow-wrap: anywhere;
  font-size: 0.8125rem;
  font-weight: 700;
}
.sp-meta {
  display: flex;
  gap: 0.5rem;
  min-width: 0;
  color: var(--text-secondary);
  font-size: 0.6875rem;
  white-space: nowrap;
}
.sp-proto {
  font-weight: 800;
}
.sp-host {
  overflow: hidden;
  text-overflow: ellipsis;
  font-family: var(--font-mono);
}
.sp-side {
  display: flex;
  align-items: center;
  gap: 0.375rem;
  flex-shrink: 0;
  font-size: 0.6875rem;
}
.sp-latency {
  color: var(--text-secondary);
  font-family: var(--font-mono);
}
.sp-action {
  color: var(--text-secondary);
}
.sp-badge {
  display: inline-flex;
  align-items: center;
  gap: 0.25rem;
  padding: 0.0625rem 0.4375rem;
  border-radius: var(--radius-badge);
  font-weight: 700;
}
.sp-badge-icon {
  width: 0.6875rem;
  height: 0.6875rem;
}
.sp-badge-connected {
  background: var(--ok-badge);
  color: var(--ok-text);
}
.sp-badge-connecting {
  background: var(--bg-sunken);
  color: var(--text-secondary);
}
.sp-badge-down {
  background: var(--alert-badge);
  color: var(--alert-text);
}
.sp-badge-broadcast {
  background: var(--broadcast-band);
  color: var(--on-broadcast);
}
.sp-empty {
  padding: 1.25rem 0.5rem;
  color: var(--text-secondary);
  font-size: 0.8125rem;
  text-align: center;
}
.sp-foot {
  display: flex;
  gap: 1rem;
  padding: 0.4375rem 0.875rem;
  border-top: 1px solid var(--border-subtle);
  color: var(--text-secondary);
  font-size: 0.6875rem;
}
</style>

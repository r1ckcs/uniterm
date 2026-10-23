<template>
  <div class="terminal-status-bar" role="status">
    <span class="tsb-item tsb-proto">{{ protocol }}</span>
    <span v-if="target" class="tsb-item tsb-mono" :title="target">{{ target }}</span>
    <span v-if="encoding" class="tsb-item">{{ encoding }}</span>
    <span v-if="size" class="tsb-item tsb-mono">{{ size }}</span>
    <span class="tsb-item" :class="{ 'tsb-down': !connected }">{{ connected ? uptime : t('tab.disconnected') }}</span>
    <span v-if="latency" class="tsb-item tsb-latency" :class="`tsb-latency-${latencyClass}`" :title="t(latencyHint)">↔ {{ latencyText }}<span v-if="latency.via === 'jump' || latency.via === 'proxy'" class="tsb-latency-via">*</span></span>
    <span class="tsb-spacer"></span>
    <span v-if="panel.outputLog?.enabled" class="tsb-flag tsb-log" :title="panel.outputLog.path">● LOG</span>
    <span v-if="broadcasting" class="tsb-flag tsb-broadcast" :title="t('terminal.broadcastInput')">{{ t('terminal.statusBroadcast') }}</span>
  </div>
</template>

<script setup lang="ts">
// Thin footer under a terminal tab: protocol · host:port · encoding ·
// cols×rows · time connected · latency (CONTRATO-latencia), plus LOG /
// broadcast flags.
import { computed, onActivated, onDeactivated, onMounted, onUnmounted, ref, watch } from 'vue'
import type { Panel } from '../types/workspace'
import { useSessionStore } from '../stores/sessionStore'
import { useTabStore } from '../stores/tabStore'
import { getTerminalSize } from '../services/terminalManager'
import { formatUptime, statusBarTarget } from '../utils/terminalStatus'
import { useLatencyStore } from '../stores/latencyStore'
import { formatLatency, latencyHintKey, latencyLevel } from '../utils/latency'
import { getShellLabel } from '../utils/shellLabel'
import { useI18n } from '../i18n'

const props = defineProps<{ panel: Panel }>()
const { t } = useI18n()
const sessionStore = useSessionStore()
const tabStore = useTabStore()

// One-second tick drives the uptime and picks up terminal resizes.
const now = ref(Date.now())
const termSize = ref({ cols: 0, rows: 0 })
function tick() {
  now.value = Date.now()
  if (props.panel.sessionId) termSize.value = getTerminalSize(props.panel.sessionId)
}
let timer: ReturnType<typeof setInterval> | null = null
onMounted(() => {
  tick()
  timer = setInterval(tick, 1000)
})
onUnmounted(() => {
  if (timer) clearInterval(timer)
})

const protocol = computed(() => props.panel.type.toUpperCase())
const target = computed(() => statusBarTarget(props.panel.type, props.panel.config, (p) => getShellLabel(p, 'Local')))
const encoding = computed(() =>
  props.panel.type === 'ssh' || props.panel.type === 'telnet' ? (props.panel.config?.encoding || 'utf-8').toUpperCase() : '')
const size = computed(() => (termSize.value.cols > 0 ? `${termSize.value.cols}×${termSize.value.rows}` : ''))
const connected = computed(() => !!props.panel.sessionId && sessionStore.getStatus(props.panel.sessionId) === 'connected')
const uptime = computed(() => {
  const at = props.panel.sessionId ? sessionStore.getConnectedAt(props.panel.sessionId) : undefined
  return at ? formatUptime(now.value - at) : ''
})
const broadcasting = computed(() => tabStore.isPanelBroadcasting(props.panel.id))

// The footer holds its session in the latency watch only while on screen:
// KeepAlive deactivates the footers of hidden tabs.
const latencyStore = useLatencyStore()
const onScreen = ref(false)
let held = ''
function syncHold() {
  const want = onScreen.value ? props.panel.sessionId || '' : ''
  if (want === held) return
  if (held) latencyStore.release(held)
  held = want
  if (held) latencyStore.hold(held)
}
watch(() => props.panel.sessionId, syncHold)
onMounted(() => { onScreen.value = true; syncHold() })
onActivated(() => { onScreen.value = true; syncHold() })
onDeactivated(() => { onScreen.value = false; syncHold() })
onUnmounted(() => { onScreen.value = false; syncHold() })

const latency = computed(() => latencyStore.sampleOf(props.panel.sessionId))
const latencyText = computed(() => formatLatency(latency.value))
const latencyClass = computed(() => latencyLevel(latency.value))
const latencyHint = computed(() => latencyHintKey(latency.value))
</script>

<style scoped>
.terminal-status-bar {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  flex-shrink: 0;
  height: 1.125rem;
  padding: 0 0.625rem;
  overflow: hidden;
  background: var(--bg-elevated);
  border-top: 1px solid var(--border-subtle);
  color: var(--text-secondary);
  font-size: 0.625rem;
  line-height: 1;
  white-space: nowrap;
  user-select: none;
}
.tsb-item {
  overflow: hidden;
  text-overflow: ellipsis;
}
.tsb-proto {
  font-weight: 800;
  color: var(--text-primary);
}
.tsb-mono {
  font-family: var(--font-mono);
}
.tsb-down {
  color: var(--error);
  font-weight: 700;
}
.tsb-spacer {
  flex: 1;
}
.tsb-latency {
  flex-shrink: 0;
  font-family: var(--font-mono);
  font-variant-numeric: tabular-nums;
}
.tsb-latency-off {
  color: var(--text-muted);
}
.tsb-latency-warn {
  color: var(--warning);
  font-weight: 700;
}
.tsb-latency-alert {
  color: var(--error);
  font-weight: 700;
}
.tsb-latency-via {
  margin-left: 0.0625rem;
}
.tsb-flag {
  flex-shrink: 0;
  font-weight: 800;
}
.tsb-log {
  color: var(--error);
}
.tsb-broadcast {
  color: var(--warning);
}
</style>

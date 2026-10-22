<template>
  <span class="csi" :class="size">
    <component :is="glyph" class="csi-glyph" />
    <span v-if="dot" class="csi-dot" :style="{ background: dot }"></span>
  </span>
</template>

<script setup lang="ts">
// Icon of a connection or group in lists: the style icon (own or inherited)
// in place of the type icon, and the color as a dot on its corner. The glyph
// keeps the surrounding text color, so states like "has a session" still
// show. Only palette tokens and validated hex reach the style attribute.
import { computed, type Component } from 'vue'
import { SquareTerminal } from '@lucide/vue'
import type { ConnectionConfig, ConnectionGroup } from '../types/session'
import { useConnectionStore } from '../stores/connectionStore'
import { colorCss, connectionStyle, groupStyle, iconComponent } from '../utils/connectionStyle'

const props = withDefaults(defineProps<{
  conn?: ConnectionConfig | null
  group?: ConnectionGroup | null
  fallback?: Component
  size?: 'sm' | 'md'
}>(), { size: 'sm' })

const connectionStore = useConnectionStore()

const style = computed(() => {
  if (props.conn) return connectionStyle(props.conn, connectionStore.groups)
  if (props.group) return groupStyle(props.group, connectionStore.groups)
  return { color: '', icon: '' }
})
const glyph = computed(() => iconComponent(style.value.icon) ?? props.fallback ?? SquareTerminal)
const dot = computed(() => colorCss(style.value.color))
</script>

<style scoped>
.csi {
  position: relative;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
}
.csi.sm .csi-glyph {
  width: 0.875rem;
  height: 0.875rem;
}
.csi.md .csi-glyph {
  width: 1rem;
  height: 1rem;
}
.csi-dot {
  position: absolute;
  right: -0.1875rem;
  bottom: -0.125rem;
  width: 0.4375rem;
  height: 0.4375rem;
  border-radius: 50%;
  box-shadow: 0 0 0 1.5px var(--bg-elevated);
}
</style>

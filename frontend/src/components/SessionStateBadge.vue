<template>
  <!-- Open-session state of a connection (item 49). Shape as well as color:
       filled dot = connected, ring = connecting, diamond = dropped. Leading
       position (on the icon / start of the row), apart from the
       reachability probe, which shows as "no response" text at the end of
       the row. The row's aria-label carries it in words. -->
  <span v-if="info" class="ssb" :class="`ssb-${info.state}`" aria-hidden="true">
    <span class="ssb-mark"></span>
    <span v-if="info.count > 1 && !hideCount" class="ssb-count">{{ info.count }}</span>
  </span>
</template>

<script setup lang="ts">
import type { ConnSessionInfo } from '../utils/sessionPicker'

// hideCount: the host shows it instead (sidebar rows, where the corner of
// the icon has no room for a number).
defineProps<{ info?: ConnSessionInfo; hideCount?: boolean }>()
</script>

<style scoped>
.ssb {
  display: inline-flex;
  align-items: center;
  gap: 0.125rem;
  line-height: 1;
}
.ssb-mark {
  box-sizing: border-box;
  width: 0.5625rem;
  height: 0.5625rem;
  flex-shrink: 0;
}
.ssb-connected .ssb-mark {
  border-radius: 50%;
  background: var(--success);
}
.ssb-connecting .ssb-mark {
  border-radius: 50%;
  border: 2px solid var(--warning);
}
.ssb-down .ssb-mark {
  width: 0.5rem;
  height: 0.5rem;
  margin: 0 0.0625rem;
  background: var(--error);
  transform: rotate(45deg);
}
.ssb-count {
  font-size: 0.625rem;
  font-weight: 800;
  color: var(--text-secondary);
}
.ssb-down .ssb-count {
  color: var(--error);
}
</style>

<template>
  <!-- deviceName comes from another device: text interpolation only, never v-html. -->
  <span
    v-if="change"
    class="vault-changed"
    :class="{ inline, risky }"
    :title="label"
    :aria-label="label"
  >
    <component :is="risky ? TriangleAlert : UserPen" :class="inline ? 'vi-sm' : 'vi-xs'" />
    <span v-if="inline">{{ label }}</span>
  </span>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { TriangleAlert, UserPen } from '@lucide/vue'
import { useI18n } from '../i18n'
import { useVaultStore } from '../stores/vaultStore'
import { formatChangedAt } from '../utils/vaultFormat'

// `inline` shows the full sentence (connection form); otherwise only the icon
// with the sentence as tooltip (connection list rows).
const props = defineProps<{ connectionId: string; inline?: boolean }>()
const { t } = useI18n()
const vault = useVaultStore()

const change = computed(() => vault.changeFor(props.connectionId))
// A remote edit of where we connect to is what the design asks to flag
// (honeypot swap); everything else is informational.
const risky = computed(() => !!change.value?.fields?.some((f) => f === 'host' || f === 'port'))
const label = computed(() => {
  const c = change.value
  if (!c) return ''
  const base = t('vault.changedBy', { device: c.deviceName, when: formatChangedAt(c.changedAt) })
  return risky.value ? `${base} — ${t('vault.changedHostPort')}` : base
})
</script>

<style scoped>
.vault-changed {
  display: inline-flex;
  align-items: center;
  gap: 0.375rem;
  flex-shrink: 0;
  color: var(--text-muted);
}
.vault-changed.risky { color: var(--warning); }
.vault-changed.inline { font-size: 0.75rem; line-height: 1.4; }
/* Icon sizes in rem so they follow the UI scale. */
.vi-sm { width: 0.875rem; height: 0.875rem; }
.vi-xs { width: 0.6875rem; height: 0.6875rem; }
</style>

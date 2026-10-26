<template>
  <div class="csp">
    <div class="csp-row" role="radiogroup" :aria-label="t('connStyle.color')">
      <button
        v-for="opt in colorOptions"
        :key="opt.value"
        type="button"
        role="radio"
        class="csp-swatch"
        :class="[{ active: colorMode === opt.value }, opt.kind]"
        :aria-checked="colorMode === opt.value"
        :aria-label="t('a11y.colorOption', { name: opt.label })"
        :title="opt.label"
        @click="pickColor(opt.value)"
      >
        <span v-if="opt.kind === 'palette'" aria-hidden="true" class="csp-dot" :style="{ background: colorCss(opt.value) }"></span>
        <span v-else class="csp-text">{{ opt.label }}</span>
      </button>
      <button
        type="button"
        role="radio"
        class="csp-swatch custom"
        :class="{ active: colorMode === 'custom' }"
        :aria-checked="colorMode === 'custom'"
        :aria-label="t('a11y.colorOption', { name: customHexValid ? `${t('connStyle.custom')} ${customHex}` : t('connStyle.custom') })"
        :title="t('connStyle.custom')"
        @click="startCustom"
      >
        <span v-if="customHexValid" class="csp-dot" :style="{ background: colorCss(customHex) }"></span>
        <span class="csp-text">#</span>
      </button>
      <el-input
        v-if="colorMode === 'custom'"
        v-model="customHex"
        class="csp-hex"
        maxlength="7"
        placeholder="#rrggbb"
        :aria-label="t('connStyle.custom')"
        @input="onHexInput"
      />
    </div>
    <div v-if="colorMode === 'custom' && customHex && !customHexValid" class="csp-error">{{ t('connStyle.hexInvalid') }}</div>

    <div class="csp-row" role="radiogroup" :aria-label="t('connStyle.icon')">
      <button
        v-for="opt in iconOptions"
        :key="opt.value"
        type="button"
        role="radio"
        class="csp-icon"
        :class="{ active: (icon || '') === opt.value }"
        :aria-checked="(icon || '') === opt.value"
        :aria-label="t('a11y.iconOption', { name: opt.label })"
        :title="opt.label"
        @click="emit('update:icon', opt.value as ConnectionIcon | '')"
      >
        <component :is="opt.comp" v-if="opt.comp" class="csp-glyph" aria-hidden="true" />
        <span v-else class="csp-text">{{ opt.label }}</span>
      </button>
    </div>

    <div class="csp-preview" :aria-label="t('connStyle.preview')">
      <span class="csp-preview-label">{{ t('connStyle.preview') }}</span>
      <span class="csp-preview-item">
        <span class="csp-preview-dot" :style="{ background: previewColor }" :class="{ empty: !previewColor }"></span>
        <component :is="previewIcon" class="csp-glyph" :style="{ color: previewColor }" />
        <span class="csp-preview-name">{{ previewName || t('connStyle.previewName') }}</span>
      </span>
      <span v-if="inheritedNote" class="csp-preview-note">{{ inheritedNote }}</span>
    </div>
  </div>
</template>

<script setup lang="ts">
// Color + icon picker shared by the connection form and the group style
// dialog. '' = inherit from the group, 'none' = nothing and do not inherit.
// The preview resolves inheritance the same way the lists do.
import { computed, ref, watch, type Component } from 'vue'
import { SquareTerminal } from '@lucide/vue'
import type { ConnectionColor, ConnectionGroup, ConnectionIcon } from '../types/session'
import { PALETTE_KEYS, ICON_KEYS, colorCss, effectiveStyle, iconComponent, normalizeColor } from '../utils/connectionStyle'
import { useI18n } from '../i18n'

const props = defineProps<{
  color?: ConnectionColor | string
  icon?: ConnectionIcon | string
  // Where inheritance starts: the connection's group, or the group's parent.
  inheritFrom?: string
  groups: ConnectionGroup[]
  previewName?: string
  // Icon shown when no style icon applies (the connection type icon).
  typeIcon?: Component
}>()

const emit = defineEmits<{
  'update:color': [v: ConnectionColor | '']
  'update:icon': [v: ConnectionIcon | '']
}>()

const { t } = useI18n()

const colorOptions = computed(() => [
  { value: '', kind: 'special', label: t('connStyle.fromGroup') },
  { value: 'none', kind: 'special', label: t('connStyle.noColor') },
  ...PALETTE_KEYS.map(k => ({ value: k, kind: 'palette', label: t(`connStyle.color.${k}`) })),
])

const iconOptions = computed(() => [
  { value: '', label: t('connStyle.fromGroup'), comp: undefined as Component | undefined },
  { value: 'none', label: t('connStyle.noIcon'), comp: undefined as Component | undefined },
  ...ICON_KEYS.map(k => ({ value: k, label: t(`connStyle.icon.${k}`), comp: iconComponent(k) })),
])

// A hex value selects the custom slot; the text box keeps what is typed
// and only a valid #rrggbb is emitted.
const customHex = ref('')
const customMode = ref(false)
const customHexValid = computed(() => /^#[0-9a-fA-F]{6}$/.test(customHex.value))
watch(() => props.color, (c) => {
  if (typeof c === 'string' && c.startsWith('#')) {
    customHex.value = c
    customMode.value = true
  }
}, { immediate: true })
const colorMode = computed(() => (customMode.value ? 'custom' : (props.color || '')))

function pickColor(v: string) {
  customMode.value = false
  emit('update:color', v as ConnectionColor | '')
}
function startCustom() {
  customMode.value = true
  if (customHexValid.value) emit('update:color', customHex.value.toLowerCase() as ConnectionColor)
}
function onHexInput() {
  const v = normalizeColor(customHex.value)
  if (v.startsWith('#')) emit('update:color', v as ConnectionColor)
}

const resolved = computed(() => effectiveStyle({ color: props.color, icon: props.icon, groupId: props.inheritFrom }, props.groups))
const previewColor = computed(() => colorCss(resolved.value.color))
const previewIcon = computed(() => iconComponent(resolved.value.icon) ?? props.typeIcon ?? SquareTerminal)
const inheritedNote = computed(() => {
  const fromGroup = (!props.color && resolved.value.color) || (!props.icon && resolved.value.icon)
  return fromGroup ? t('connStyle.inherited') : ''
})
</script>

<style scoped>
.csp {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
  width: 100%;
}
.csp-row {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.25rem;
}
.csp-swatch,
.csp-icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 0.25rem;
  min-width: 1.75rem;
  height: 1.75rem;
  padding: 0 0.375rem;
  border: 1px solid var(--border-field);
  border-radius: var(--radius-row);
  background: var(--bg-input);
  color: var(--text-primary);
  font: inherit;
  font-size: 0.6875rem;
  cursor: pointer;
}
.csp-swatch:hover,
.csp-icon:hover {
  border-color: var(--border-field-hover);
}
.csp-swatch.active,
.csp-icon.active {
  border-color: var(--accent);
  background: var(--bg-active);
  font-weight: 700;
}
.csp-dot {
  width: 0.875rem;
  height: 0.875rem;
  border-radius: 50%;
}
.csp-glyph {
  width: 0.9375rem;
  height: 0.9375rem;
}
.csp-text {
  white-space: nowrap;
}
.csp-hex {
  width: 7rem;
}
.csp-error {
  font-size: 0.6875rem;
  color: var(--error);
}
.csp-preview {
  display: flex;
  align-items: center;
  gap: 0.625rem;
  font-size: 0.75rem;
  color: var(--text-secondary);
}
.csp-preview-item {
  display: inline-flex;
  align-items: center;
  gap: 0.375rem;
  padding: 0.25rem 0.625rem;
  border-radius: var(--radius-row);
  background: var(--bg-sunken);
  color: var(--text-primary);
}
.csp-preview-dot {
  width: 0.5rem;
  height: 0.5rem;
  border-radius: 50%;
}
.csp-preview-dot.empty {
  display: none;
}
.csp-preview-name {
  font-weight: 600;
}
.csp-preview-note {
  font-size: 0.6875rem;
  color: var(--text-muted);
}
</style>

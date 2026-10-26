<template>
  <!-- line: what fits, cut in the middle (first, so the row's baseline is
       its baseline); sizer: invisible full text, so a shrink-to-fit parent
       (tab) still sizes for the whole name; sr: the full name for screen
       readers. -->
  <span ref="el" class="mid-ellipsis" :title="noTitle ? undefined : text">
    <span ref="line" class="me-line" aria-hidden="true">{{ shown }}</span>
    <span ref="sizer" class="me-sizer" aria-hidden="true">{{ text }}</span>
    <span class="me-sr">{{ text }}</span>
  </span>
</template>

<script lang="ts">
// Shared by every instance: one ResizeObserver and one measuring canvas,
// widths cached by font + string. Text is measured only when a box changes
// width (sidebar drag, window resize) or the name changes, never per render.
const callbacks = new Map<Element, () => void>()
let observer: ResizeObserver | null = null
let fontsHooked = false
function observe(el: Element, cb: () => void) {
  if (typeof ResizeObserver === 'undefined') return
  observer ??= new ResizeObserver((entries) => { for (const e of entries) callbacks.get(e.target)?.() })
  callbacks.set(el, cb)
  observer.observe(el)
  // A web font that arrives later changes every width: refit all once more.
  if (!fontsHooked && document.fonts) {
    fontsHooked = true
    const refitAll = () => { widths.clear(); callbacks.forEach((f) => f()) }
    document.fonts.ready.then(refitAll)
    document.fonts.addEventListener?.('loadingdone', refitAll)
  }
}
function unobserve(el: Element) {
  callbacks.delete(el)
  observer?.unobserve(el)
}

let ctx: CanvasRenderingContext2D | null = null
const widths = new Map<string, number>()
function measurer(font: string): (s: string) => number {
  if (!ctx) ctx = document.createElement('canvas').getContext('2d')
  return (s: string) => {
    const key = font + '\u0000' + s
    let w = widths.get(key)
    if (w === undefined) {
      if (widths.size > 20000) widths.clear()
      ctx!.font = font
      w = ctx!.measureText(s).width
      widths.set(key, w)
    }
    return w
  }
}
</script>

<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { fitMiddle } from '../utils/middleEllipsis'

const props = defineProps<{ text: string; noTitle?: boolean }>()
const el = ref<HTMLElement | null>(null)
const line = ref<HTMLElement | null>(null)
const sizer = ref<HTMLElement | null>(null)
const shown = ref(props.text)

function fit() {
  const box = el.value
  const ln = line.value
  if (!box || !ln) return
  const width = box.clientWidth
  if (!width) return
  // Layout decides "fits whole" (a shrink-to-fit tab is exactly as wide as
  // the name); the canvas only sizes the cut.
  if (sizer.value && sizer.value.scrollWidth <= width) {
    shown.value = props.text
    return
  }
  const cs = getComputedStyle(ln)
  // 1 px of slack: canvas and layout can round differently.
  shown.value = fitMiddle(props.text, width - 1, measurer(`${cs.fontStyle} ${cs.fontWeight} ${cs.fontSize} ${cs.fontFamily}`))
}

watch(() => props.text, (t) => { shown.value = t; nextTick(fit) })
onMounted(() => {
  if (el.value) observe(el.value, fit)
  fit()
})
onBeforeUnmount(() => { if (el.value) unobserve(el.value) })
</script>

<style scoped>
.mid-ellipsis {
  display: block;
  min-width: 0;
  max-width: 100%;
  overflow: hidden;
  white-space: nowrap;
}
.me-sizer {
  display: block;
  height: 0;
  overflow: hidden;
  visibility: hidden;
}
.me-line {
  display: block;
  overflow: hidden;
}
.me-sr {
  position: absolute;
  width: 1px;
  height: 1px;
  overflow: hidden;
  clip-path: inset(50%);
  white-space: nowrap;
}
</style>

<template>
  <!-- The full name stays in the text (screen readers read it whole) and in
       the title; only the head shrinks with an ellipsis. -->
  <span class="mid-ellipsis" :title="noTitle ? undefined : text"><span class="me-head">{{ parts.head }}</span><span v-if="parts.tail" class="me-tail">{{ parts.tail }}</span></span>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { splitMiddle } from '../utils/middleEllipsis'

const props = defineProps<{ text: string; noTitle?: boolean }>()
const parts = computed(() => splitMiddle(props.text))
</script>

<style scoped>
.mid-ellipsis {
  display: inline-flex;
  min-width: 0;
  max-width: 100%;
  overflow: hidden;
  white-space: nowrap;
  vertical-align: bottom;
}
/* The tail never shrinks (unless it alone is wider than the box, then it
   gets an end ellipsis); the head takes the whole cut. */
.me-head {
  flex: 0 1 auto;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
}
.me-tail {
  flex: 0 0 auto;
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
}
</style>

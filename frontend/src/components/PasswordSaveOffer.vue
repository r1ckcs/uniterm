<template>
  <el-dialog
    append-to-body
    :model-value="!!current"
    :title="current?.update ? t('pwsave.updateTitle') : t('pwsave.title')"
    width="27rem"
    :close-on-click-modal="false"
    @update:model-value="(v: boolean) => { if (!v) answer('no') }"
  >
    <p v-if="current" class="pw-offer-text">
      {{ current.update ? t('pwsave.updateText', { target: target(current) }) : t('pwsave.text', { target: target(current) }) }}
    </p>
    <template #footer>
      <el-button @click="answer('never')">{{ t('pwsave.never') }}</el-button>
      <el-button @click="answer('no')">{{ t('pwsave.no') }}</el-button>
      <el-button type="primary" @click="answer('save')">{{ current?.update ? t('pwsave.update') : t('pwsave.save') }}</el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
// MobaXterm-style "save password?" offer, shown after an SSH-family login
// succeeded with a password that differs from the saved one. The password
// itself never reaches the webview: the backend keeps it and this dialog only
// answers the offer by session id.
import { computed, onBeforeUnmount, ref } from 'vue'
import { Events } from '@wailsio/runtime'
import { ElMessage } from 'element-plus'
import { useI18n } from '../i18n'
import { ResolvePasswordOffer } from '../../bindings/github.com/ys-ll/uniterm/app'

interface PasswordOffer {
  sessionId: string
  connectionId: string
  name: string
  user: string
  host: string
  update: boolean
}

const { t } = useI18n()
const queue = ref<PasswordOffer[]>([])
const current = computed(() => queue.value[0] ?? null)

function target(o: PasswordOffer): string {
  const where = o.user ? `${o.user}@${o.host}` : o.host
  return o.name && o.name !== o.host ? `${o.name} (${where})` : where
}

async function answer(action: 'save' | 'no' | 'never') {
  const o = queue.value.shift()
  if (!o) return
  try {
    await ResolvePasswordOffer(o.sessionId, action)
    if (action === 'save') ElMessage.success(t('pwsave.saved'))
  } catch (e) {
    if (action !== 'no') ElMessage.error(String(e))
  }
}

const unsub = Events.On('session:password-offer', (ev) => {
  const o = ev.data as PasswordOffer
  if (!o?.sessionId || queue.value.some(q => q.sessionId === o.sessionId)) return
  // One pending offer per connection: a newer login replaces an older one.
  queue.value = [...queue.value.filter(q => q.connectionId !== o.connectionId), o]
})
onBeforeUnmount(() => unsub())
</script>

<style scoped>
.pw-offer-text { margin: 0; line-height: 1.5; word-break: break-word; }
</style>

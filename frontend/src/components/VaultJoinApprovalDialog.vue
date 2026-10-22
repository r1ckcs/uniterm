<template>
  <el-dialog append-to-body
    :model-value="!!req || !!conf"
    @update:model-value="(v: boolean) => !v && onLater()"
    :title="conf ? t('vault.join.confirmTitle') : t('vault.join.title')"
    width="27.5rem"
    class="vault-dialog"
    :close-on-click-modal="false"
  >
    <!-- Step 3: the 4 digits that close the loop on the new device. -->
    <div v-if="conf" class="vault-step vault-center">
      <p class="vault-lead">{{ t('vault.join.confirmLead', { name: conf.name }) }}</p>
      <div class="vault-code" :aria-label="t('vault.join.confirmTitle')">{{ conf.code4 }}</div>
      <p class="vault-muted">{{ t('vault.join.confirmHint') }}</p>
    </div>

    <div v-else-if="req" class="vault-step">
      <!-- Remote text: interpolation only, never v-html. -->
      <p class="vault-lead">{{ t('vault.join.lead') }}</p>
      <dl class="vault-facts">
        <dt>{{ t('vault.join.deviceName') }}</dt><dd class="vault-remote-name">{{ req.name }}</dd>
        <dt>{{ t('vault.fingerprint') }}</dt><dd class="mono">{{ req.fingerprint }}</dd>
        <dt v-if="expiresIn">{{ t('vault.approval.expires') }}</dt><dd v-if="expiresIn">{{ expiresIn }}</dd>
      </dl>

      <p v-if="!begun" class="vault-muted">{{ t('vault.join.notYou') }}</p>

      <div v-else-if="!req.ready" class="vault-waiting">
        <el-icon class="vault-spin" :size="18"><Loader2 /></el-icon>
        <span>{{ t('vault.join.waitingCode') }}</span>
      </div>

      <template v-else>
        <label class="vault-code-label" for="vault-join-code">{{ t('vault.join.typeCode') }}</label>
        <el-input
          id="vault-join-code"
          ref="codeInput"
          v-model="code"
          class="vault-code-input"
          inputmode="numeric"
          maxlength="7"
          autocomplete="off"
          placeholder="000 000"
          @input="onCodeInput"
          @keyup.enter="onApprove"
        />
        <div class="vault-role-row">
          <span class="vault-muted">{{ t('vault.join.role') }}</span>
          <el-select v-model="role" size="small" class="vault-role">
            <el-option value="admin" :label="t('vault.role.admin')" />
            <el-option value="writer" :label="t('vault.role.writer')" />
            <el-option value="reader" :label="t('vault.role.reader')" />
          </el-select>
        </div>
        <p class="vault-muted">{{ t('vault.join.attemptsLeft', { n: req.attemptsLeft }) }}</p>
      </template>

      <p v-if="vault.lastError" class="vault-error">{{ errorText(vault.lastError) }}</p>
    </div>

    <template #footer>
      <el-button v-if="conf" type="primary" @click="vault.closeConfirmation()">{{ t('vault.close') }}</el-button>
      <template v-else>
      <el-button @click="onReject">{{ t('vault.join.reject') }}</el-button>
      <el-button v-if="!begun" type="primary" :loading="vault.busy" @click="onBegin">{{ t('vault.join.approve') }}</el-button>
      <el-button v-else-if="req?.ready" type="primary" :loading="vault.busy" :disabled="digits.length !== 6" @click="onApprove">{{ t('vault.join.confirm') }}</el-button>
      </template>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { computed, nextTick, onUnmounted, ref, watch } from 'vue'
import { Loader2 } from '@lucide/vue'
import { useI18n } from '../i18n'
import { useVaultStore } from '../stores/vaultStore'
import type { VaultRole } from '../types/vault'
import { formatCountdown } from '../utils/vaultFormat'
import { vaultErrorText } from '../utils/vaultErrors'

const { t } = useI18n()
const vault = useVaultStore()
const errorText = (c: string) => vaultErrorText(t, c)

const req = computed(() => vault.joinRequest)
const conf = computed(() => vault.confirmation)
const begun = ref(false)
const code = ref('')
const role = ref<VaultRole>('admin')
const codeInput = ref<{ focus: () => void } | null>(null)
const digits = computed(() => code.value.replace(/\D/g, ''))

const now = ref(Date.now())
const tick = setInterval(() => { now.value = Date.now() }, 1000)
onUnmounted(() => clearInterval(tick))
const expiresIn = computed(() => (req.value ? formatCountdown(req.value.expiresAt, now.value) : ''))

watch(() => req.value?.requestId, () => {
  begun.value = false
  code.value = ''
  role.value = 'admin'
  vault.clearError()
})
watch(() => req.value?.ready, async (r) => {
  if (r) {
    begun.value = true
    await nextTick()
    codeInput.value?.focus()
  }
})

// "482915" or "482 915", nothing else.
function onCodeInput(v: string) {
  const d = v.replace(/\D/g, '').slice(0, 6)
  code.value = d.length > 3 ? `${d.slice(0, 3)} ${d.slice(3)}` : d
}

async function onBegin() {
  if (await vault.beginApproval()) begun.value = true
}

async function onApprove() {
  if (digits.value.length !== 6) return
  if (!(await vault.approveJoin(digits.value, role.value))) code.value = ''
}

async function onReject() {
  await vault.rejectJoin()
}

// × keeps the request pending on the backend; it reappears from Settings → Vault.
function onLater() {
  if (conf.value) return vault.closeConfirmation()
  if (req.value) vault.snoozeJoin(req.value.requestId)
}
</script>

<style scoped>
.vault-step { display: flex; flex-direction: column; gap: 0.75rem; }
.vault-center { align-items: center; text-align: center; }
.vault-code {
  font-family: var(--font-mono);
  font-size: 2.75rem;
  font-weight: 600;
  letter-spacing: 0.16em;
  color: var(--text-primary);
  padding: 0.5rem 1.25rem;
  border-radius: var(--radius-md);
  background: var(--bg-surface);
  border: 1px solid var(--border-subtle);
}
.vault-lead { margin: 0; color: var(--text-primary); font-size: 0.875rem; line-height: 1.55; }
.vault-muted { margin: 0; color: var(--text-secondary); font-size: 0.8125rem; line-height: 1.5; }
.vault-error { margin: 0; color: var(--error); font-size: 0.8125rem; }
.vault-facts { display: grid; grid-template-columns: auto 1fr; gap: 0.25rem 0.75rem; margin: 0; font-size: 0.8125rem; }
.vault-facts dt { color: var(--text-muted); }
.vault-facts dd { margin: 0; color: var(--text-primary); overflow-wrap: anywhere; }
.vault-remote-name { font-weight: 600; }
.mono { font-family: var(--font-mono); }
.vault-waiting { display: flex; align-items: center; gap: 0.5rem; color: var(--text-secondary); font-size: 0.8125rem; }
.vault-spin { color: var(--accent); animation: vault-rotate 1s linear infinite; }
@keyframes vault-rotate { to { transform: rotate(360deg); } }
.vault-code-label { color: var(--text-primary); font-size: 0.8125rem; }
.vault-code-input { width: 13rem; align-self: center; }
.vault-code-label { align-self: center; }
.vault-code-input :deep(input) {
  font-family: var(--font-mono);
  font-size: 1.75rem;
  letter-spacing: 0.12em;
  text-align: center;
  height: 3.25rem;
}
.vault-role-row { display: flex; align-items: center; gap: 0.75rem; }
.vault-role { width: 9rem; }
</style>

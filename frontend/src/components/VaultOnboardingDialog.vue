<template>
  <el-dialog append-to-body
    :model-value="vault.onboardingVisible"
    @update:model-value="(v: boolean) => !v && onClose()"
    :title="title"
    width="32.5rem"
    class="vault-dialog"
    :close-on-click-modal="false"
    :close-on-press-escape="closable"
    :show-close="closable"
  >
    <div v-if="vault.status.testMode" class="vault-test-band">{{ t('vault.testMode') }}</div>

    <!-- Welcome -->
    <div v-if="step === 'welcome'" class="vault-step">
      <div class="vault-hero"><Cloud class="vi-xxl" /></div>
      <p class="vault-lead">{{ t('vault.welcome.lead') }}</p>
      <ul class="vault-points">
        <li><Lock class="vi-sm" /><span>{{ t('vault.welcome.pointEncrypted') }}</span></li>
        <li><MonitorSmartphone class="vi-sm" /><span>{{ t('vault.welcome.pointDevices') }}</span></li>
        <li><WifiOff class="vi-sm" /><span>{{ t('vault.welcome.pointOptional') }}</span></li>
      </ul>
      <p v-if="vault.lastError" class="vault-error">{{ errorText(vault.lastError) }}</p>
    </div>

    <!-- Browser open / creating -->
    <div v-else-if="step === 'browser' || step === 'creating'" class="vault-step vault-center">
      <el-icon class="vault-spin" :size="28"><Loader2 /></el-icon>
      <p class="vault-lead">{{ step === 'browser' ? t('vault.browser.lead') : t('vault.creating') }}</p>
      <p v-if="step === 'browser'" class="vault-muted">{{ t('vault.browser.hint') }}</p>
    </div>

    <!-- 24 words -->
    <div v-else-if="step === 'words'" class="vault-step">
      <p class="vault-lead">{{ t('vault.recovery.lead') }}</p>
      <ol class="vault-words" :aria-label="t('vault.recovery.title')">
        <li v-for="(w, i) in words" :key="i"><span class="vault-word-n">{{ i + 1 }}</span><span class="vault-word">{{ w }}</span></li>
      </ol>
      <div class="vault-warn"><TriangleAlert class="vi-sm" /><span>{{ t('vault.recovery.warn') }}</span></div>
      <p v-if="regen" class="vault-muted">{{ t('vault.recovery.regenNote') }}</p>
      <el-checkbox v-model="wroteDown">{{ t('vault.recovery.wroteDown') }}</el-checkbox>
    </div>

    <!-- Confirm 3 words -->
    <div v-else-if="step === 'confirm'" class="vault-step">
      <p class="vault-lead">{{ t('vault.confirm.lead') }}</p>
      <p v-if="regen" class="vault-muted">{{ t('vault.recovery.regenNote') }}</p>
      <div class="vault-confirm">
        <label v-for="(pos, i) in checkPositions" :key="pos" class="vault-confirm-row">
          <span class="vault-confirm-label">{{ t('vault.confirm.word', { n: pos + 1 }) }}</span>
          <el-input
            v-model="confirmWords[i]"
            autocomplete="off"
            autocapitalize="off"
            :spellcheck="false"
            @keyup.enter="onConfirm"
          />
        </label>
      </div>
      <p v-if="vault.lastError" class="vault-error">{{ errorText(vault.lastError) }}</p>
    </div>

    <!-- New device: waiting for approval -->
    <div v-else-if="step === 'approval'" class="vault-step vault-center">
      <template v-if="pending?.approved">
        <p class="vault-lead">{{ t('vault.approval.leadConfirm') }}</p>
        <el-input
          ref="code4Input"
          v-model="code4"
          class="vault-code4-input"
          inputmode="numeric"
          maxlength="4"
          autocomplete="off"
          placeholder="0000"
          @input="(v: string) => (code4 = v.replace(/\D/g, '').slice(0, 4))"
          @keyup.enter="onComplete"
        />
        <p class="vault-muted">{{ t('vault.approval.onlyOther') }}</p>
        <p class="vault-muted">{{ t('vault.join.attemptsLeft', { n: pending.attemptsLeft }) }}</p>
        <p v-if="vault.lastError" class="vault-error">{{ errorText(vault.lastError) }}</p>
      </template>
      <template v-else-if="pending?.code6">
        <p class="vault-lead">{{ t('vault.approval.leadCode') }}</p>
        <div class="vault-code" :aria-label="t('vault.approval.codeLabel')">{{ groupCode(pending.code6) }}</div>
        <p class="vault-muted">{{ t('vault.approval.hintCode') }}</p>
      </template>
      <template v-else>
        <el-icon class="vault-spin" :size="28"><Loader2 /></el-icon>
        <p class="vault-lead">{{ t('vault.approval.lead') }}</p>
      </template>
      <dl class="vault-facts">
        <dt>{{ t('vault.thisDevice') }}</dt><dd>{{ vault.status.device.name }}</dd>
        <dt>{{ t('vault.fingerprint') }}</dt><dd class="mono">{{ pending?.fingerprint }}</dd>
        <dt v-if="pending?.code6 && !pending.approved && expiresIn">{{ t('vault.approval.expires') }}</dt><dd v-if="pending?.code6 && !pending.approved && expiresIn">{{ expiresIn }}</dd>
      </dl>
      <el-button v-if="!pending?.approved" link type="primary" @click="step = 'recover'">{{ t('vault.approval.useWords') }}</el-button>
    </div>

    <!-- New device: recover with the 24 words -->
    <div v-else-if="step === 'recover'" class="vault-step">
      <p class="vault-lead">{{ t('vault.recover.lead') }}</p>
      <el-input
        v-model="recoverText"
        type="textarea"
        :rows="5"
        autocomplete="off"
        autocapitalize="off"
        :spellcheck="false"
        :placeholder="t('vault.recover.placeholder')"
        class="vault-recover-input"
      />
      <div class="vault-recover-count" :class="{ ok: recoverWords.length === RECOVERY_WORD_COUNT }">
        {{ t('vault.recover.count', { n: recoverWords.length, total: RECOVERY_WORD_COUNT }) }}
      </div>
      <p v-if="vault.lastError" class="vault-error">{{ errorText(vault.lastError) }}</p>
    </div>

    <!-- This device was removed -->
    <div v-else-if="step === 'revoked'" class="vault-step">
      <div class="vault-alert"><ShieldOff class="vi-lg" /><span>{{ t('vault.revoked.lead') }}</span></div>
      <p class="vault-muted">{{ t('vault.revoked.hint') }}</p>
    </div>

    <!-- Integrity / blocking error -->
    <div v-else-if="step === 'error'" class="vault-step">
      <div class="vault-alert"><ShieldAlert class="vi-lg" /><span>{{ vault.integrityAlert ? t('vault.integrity.lead') : errorText(vault.status.error?.code ?? '') }}</span></div>
      <p v-if="vault.integrityAlert" class="vault-muted">{{ t('vault.integrity.hint') }}</p>
    </div>

    <template #footer>
      <template v-if="step === 'welcome'">
        <el-button @click="onSkip">{{ t('vault.welcome.notNow') }}</el-button>
        <el-button type="primary" :loading="vault.busy" @click="vault.signIn()">
          {{ vault.status.testMode ? t('vault.welcome.signInTest') : t('vault.welcome.signIn') }}
        </el-button>
      </template>
      <el-button v-else-if="step === 'browser'" @click="vault.cancelSignIn()">{{ t('common.cancel') }}</el-button>
      <template v-else-if="step === 'words'">
        <el-button v-if="regen" @click="onCancelRegen">{{ t('common.cancel') }}</el-button>
        <el-button type="primary" :disabled="!wroteDown" @click="goConfirm">{{ t('vault.recovery.next') }}</el-button>
      </template>
      <template v-else-if="step === 'confirm'">
        <el-button v-if="regen" @click="onCancelRegen">{{ t('common.cancel') }}</el-button>
        <el-button @click="step = 'words'">{{ t('vault.confirm.back') }}</el-button>
        <el-button type="primary" :loading="vault.busy" :disabled="confirmWords.some((w) => !w.trim())" @click="onConfirm">{{ regen ? t('vault.confirm.submitRegen') : t('vault.confirm.submit') }}</el-button>
      </template>
      <template v-else-if="step === 'approval'">
        <el-button @click="vault.cancelJoin()">{{ t('vault.approval.cancel') }}</el-button>
        <el-button v-if="pending?.approved" type="primary" :loading="vault.busy" :disabled="code4.length !== 4" @click="onComplete">{{ t('vault.approval.finish') }}</el-button>
      </template>
      <template v-else-if="step === 'recover'">
        <el-button @click="backToApproval">{{ t('vault.recover.back') }}</el-button>
        <el-button type="primary" :loading="vault.busy" :disabled="recoverWords.length !== RECOVERY_WORD_COUNT" @click="onRecover">{{ t('vault.recover.submit') }}</el-button>
      </template>
      <template v-else-if="step === 'revoked'">
        <el-button @click="onClose">{{ t('vault.close') }}</el-button>
        <el-button type="primary" @click="vault.signOut()">{{ t('vault.signOut') }}</el-button>
      </template>
      <el-button v-else-if="step === 'error'" type="primary" @click="onClose">{{ t('vault.close') }}</el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { computed, nextTick, onUnmounted, ref, watch } from 'vue'
import { Cloud, Lock, MonitorSmartphone, WifiOff, Loader2, TriangleAlert, ShieldOff, ShieldAlert } from '@lucide/vue'
import { useI18n } from '../i18n'
import { useVaultStore, parseRecoveryWords } from '../stores/vaultStore'
import { RECOVERY_WORD_COUNT } from '../types/vault'
import { groupCode, formatCountdown } from '../utils/vaultFormat'
import { vaultErrorText } from '../utils/vaultErrors'

type Step = 'welcome' | 'browser' | 'creating' | 'words' | 'confirm' | 'approval' | 'recover' | 'revoked' | 'error'

const { t } = useI18n()
const vault = useVaultStore()
const errorText = (code: string) => vaultErrorText(t, code)

const step = ref<Step>('welcome')
const words = ref<string[]>([])
const wroteDown = ref(false)
const confirmWords = ref<string[]>([])
const recoverText = ref('')
const code4 = ref('')
const code4Input = ref<{ focus: () => void } | null>(null)
const now = ref(Date.now())
const tick = setInterval(() => { now.value = Date.now() }, 1000)
onUnmounted(() => clearInterval(tick))

const pending = computed(() => vault.status.pendingJoin)
const regen = computed(() => !!vault.status.recoveryRegen)
const checkPositions = computed(() => vault.status.recoveryCheck ?? [])
const recoverWords = computed(() => parseRecoveryWords(recoverText.value))
const expiresIn = computed(() => (pending.value ? formatCountdown(pending.value.expiresAt, now.value) : ''))
// Only screens with nothing at stake can be dismissed with Esc / ×.
const closable = computed(() => ['welcome', 'revoked', 'error'].includes(step.value))

const title = computed(() => ({
  welcome: t('vault.welcome.title'),
  browser: t('vault.browser.title'),
  creating: t('vault.browser.title'),
  words: regen.value ? t('vault.recovery.regenTitle') : t('vault.recovery.title'),
  confirm: t('vault.confirm.title'),
  approval: t('vault.approval.title'),
  recover: t('vault.recover.title'),
  revoked: t('vault.revoked.title'),
  error: vault.integrityAlert ? t('vault.integrity.title') : t('vault.title'),
}[step.value]))

// The backend phase decides the screen; the dialog only adds the sub-steps
// (words → confirm, approval → recover) that the phase does not distinguish.
watch(() => vault.status.phase, async (p) => {
  switch (p) {
    case 'off': step.value = 'welcome'; break
    case 'signingIn': step.value = 'browser'; break
    case 'creating': step.value = 'creating'; break
    case 'showRecovery':
      if (step.value !== 'words' && step.value !== 'confirm') {
        wroteDown.value = false
        words.value = await vault.recoveryWords()
        step.value = 'words'
      }
      break
    case 'awaitingApproval': if (step.value !== 'recover') step.value = 'approval'; break
    case 'revoked': step.value = 'revoked'; break
    case 'error': step.value = 'error'; break
    // Reset the sub-step too: a later regeneration must fetch fresh words.
    case 'ready': wipeSecrets(); step.value = 'welcome'; break
  }
}, { immediate: true })

function goConfirm() {
  vault.clearError()
  confirmWords.value = checkPositions.value.map(() => '')
  step.value = 'confirm'
}

async function onConfirm() {
  if (confirmWords.value.some((w) => !w.trim())) return
  if (await vault.confirmRecovery(confirmWords.value.map((w) => w.trim().toLowerCase()))) wipeSecrets()
}

async function onComplete() {
  if (code4.value.length !== 4) return
  if (await vault.completeJoin(code4.value)) wipeSecrets()
  else code4.value = ''
}

async function onCancelRegen() {
  await vault.cancelRecovery()
  wipeSecrets()
}

async function onRecover() {
  if (await vault.recoverWithWords(recoverWords.value)) wipeSecrets()
}

function backToApproval() {
  recoverText.value = ''
  vault.clearError()
  step.value = 'approval'
}

// The words must not linger in component state once the vault is set up.
function wipeSecrets() {
  words.value = []
  confirmWords.value = []
  recoverText.value = ''
  code4.value = ''
  wroteDown.value = false
}

watch(() => vault.status.pendingJoin?.approved, async (a) => {
  if (!a) return
  vault.clearError()
  await nextTick()
  code4Input.value?.focus()
})

async function onSkip() {
  await vault.skip()
}

function onClose() {
  if (step.value === 'welcome' && vault.status.promptWelcome) return void onSkip()
  vault.closeOnboarding()
}
</script>

<style scoped>
.vault-test-band {
  margin: -0.5rem 0 1rem;
  padding: 0.375rem 0.75rem;
  border-radius: var(--radius-sm);
  background: var(--warning-subtle);
  color: var(--warning);
  font-size: 0.75rem;
  font-weight: 600;
}
.vault-step { display: flex; flex-direction: column; gap: 0.875rem; }
.vault-center { align-items: center; text-align: center; }
.vault-hero {
  align-self: flex-start;
  display: flex;
  padding: 0.625rem;
  border-radius: var(--radius-md);
  background: var(--accent-subtle);
  color: var(--accent-text);
}
.vault-lead { margin: 0; color: var(--text-primary); font-size: 0.875rem; line-height: 1.55; }
.vault-muted { margin: 0; color: var(--text-secondary); font-size: 0.8125rem; line-height: 1.5; }
.vault-points { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 0.5rem; }
.vault-points li { display: flex; gap: 0.5rem; align-items: flex-start; color: var(--text-secondary); font-size: 0.8125rem; line-height: 1.45; }
.vault-points li svg { flex-shrink: 0; margin-top: 0.125rem; color: var(--accent-text); }
.vault-error { margin: 0; color: var(--error); font-size: 0.8125rem; }
.vault-spin { color: var(--accent-text); animation: vault-rotate 1s linear infinite; margin-top: 0.5rem; }
@keyframes vault-rotate { to { transform: rotate(360deg); } }

.vault-words {
  list-style: none;
  margin: 0;
  padding: 0.75rem;
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 0.375rem 0.75rem;
  background: var(--bg-surface);
  border: 1px solid var(--border-subtle);
  border-radius: var(--radius-md);
  user-select: text;
}
.vault-words li { display: flex; gap: 0.375rem; align-items: baseline; font-family: var(--font-mono); font-size: 0.8125rem; }
.vault-word-n { color: var(--text-muted); font-size: 0.6875rem; min-width: 1.125rem; text-align: right; }
.vault-word { color: var(--text-primary); }
.vault-warn {
  display: flex;
  gap: 0.5rem;
  align-items: flex-start;
  padding: 0.5rem 0.75rem;
  border-radius: var(--radius-sm);
  background: var(--warning-subtle);
  color: var(--text-primary);
  font-size: 0.8125rem;
  line-height: 1.45;
}
.vault-warn svg { flex-shrink: 0; margin-top: 0.125rem; color: var(--warning); }

.vault-confirm { display: flex; flex-direction: column; gap: 0.5rem; }
.vault-confirm-row { display: flex; align-items: center; gap: 0.75rem; }
.vault-confirm-label { min-width: 7rem; color: var(--text-secondary); font-size: 0.8125rem; }

.vault-code {
  font-family: var(--font-mono);
  font-size: 2.75rem;
  font-weight: 600;
  letter-spacing: 0.12em;
  color: var(--text-primary);
  padding: 0.5rem 1.25rem;
  border-radius: var(--radius-md);
  background: var(--bg-surface);
  border: 1px solid var(--border-subtle);
  user-select: all;
}
.vault-facts { display: grid; grid-template-columns: auto auto; gap: 0.25rem 0.75rem; margin: 0; font-size: 0.8125rem; text-align: left; }
.vault-facts dt { color: var(--text-muted); }
.vault-facts dd { margin: 0; color: var(--text-primary); }
.mono { font-family: var(--font-mono); }

.vault-code4-input { width: 9rem; }
.vault-code4-input :deep(input) {
  font-family: var(--font-mono);
  font-size: 1.75rem;
  letter-spacing: 0.2em;
  text-align: center;
  height: 3.25rem;
}
.vault-recover-input :deep(textarea) { font-family: var(--font-mono); }
.vault-recover-count { font-size: 0.75rem; color: var(--text-muted); margin-top: -0.375rem; }
.vault-recover-count.ok { color: var(--success); }

.vault-alert {
  display: flex;
  gap: 0.625rem;
  align-items: flex-start;
  padding: 0.75rem;
  border-radius: var(--radius-md);
  background: var(--error-glow);
  color: var(--text-primary);
  font-size: 0.875rem;
  font-weight: 500;
  line-height: 1.5;
}
.vault-alert svg { flex-shrink: 0; color: var(--error); margin-top: 0.0625rem; }
/* Icon sizes in rem so they follow the UI scale. */
.vi-lg { width: 1.125rem; height: 1.125rem; }
.vi-sm { width: 0.875rem; height: 0.875rem; }
.vi-xxl { width: 2rem; height: 2rem; }
</style>

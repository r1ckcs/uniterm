<template>
  <div class="vault-settings">
    <h2 class="section-title">{{ t('vault.title') }}</h2>
    <p class="section-desc">{{ t('vault.settings.desc') }}</p>
    <div v-if="s.testMode" class="vault-test-band">{{ t('vault.testMode') }}</div>

    <!-- Not signed in -->
    <div v-if="s.phase === 'off'" class="vault-card">
      <div class="vault-card-body vault-empty">
        <Cloud class="vault-empty-icon vi-xl" />
        <p class="vault-muted">{{ t('vault.settings.offDesc') }}</p>
        <el-button type="primary" :loading="vault.busy" @click="vault.signIn()">
          {{ s.testMode ? t('vault.welcome.signInTest') : t('vault.welcome.signIn') }}
        </el-button>
        <p v-if="vault.lastError || s.error" class="vault-error">{{ errorText(vault.lastError || s.error?.code || '') }}</p>
      </div>
    </div>

    <!-- Setup in progress: the onboarding dialog owns it -->
    <div v-else-if="inSetup" class="vault-card">
      <div class="vault-card-body vault-empty">
        <p class="vault-muted">{{ t('vault.settings.inSetup') }}</p>
        <el-button @click="vault.openOnboarding()">{{ t('vault.settings.continueSetup') }}</el-button>
      </div>
    </div>

    <!-- Revoked / blocking error -->
    <div v-else-if="s.phase === 'revoked' || s.phase === 'error'" class="vault-card">
      <div class="vault-card-body">
        <div class="vault-alert">
          <component :is="s.phase === 'revoked' ? ShieldOff : ShieldAlert" class="vi-lg" />
          <span>{{ s.phase === 'revoked' ? t('vault.revoked.lead') : vault.integrityAlert ? t('vault.integrity.lead') : errorText(s.error?.code ?? '') }}</span>
        </div>
        <p class="vault-muted vault-gap">{{ s.phase === 'revoked' ? t('vault.revoked.hint') : t('vault.integrity.hint') }}</p>
        <el-button @click="onSignOut">{{ t('vault.signOut') }}</el-button>
      </div>
    </div>

    <template v-else>
      <!-- Account + this device -->
      <div class="vault-card">
        <div class="vault-card-header">{{ t('vault.settings.account') }}</div>
        <div class="vault-card-body">
          <div class="vault-rows">
            <div class="vault-row">
              <span class="vault-label">{{ s.testMode ? t('vault.settings.folder') : t('vault.settings.google') }}</span>
              <span class="vault-value mono">{{ s.account?.email }}</span>
            </div>
            <div class="vault-row">
              <span class="vault-label">{{ t('vault.thisDevice') }}</span>
              <template v-if="renaming">
                <el-input v-model="newName" size="small" maxlength="64" class="vault-rename" @keyup.enter="saveName" @keyup.esc="renaming = false" />
                <el-button size="small" type="primary" :disabled="!newName.trim()" @click="saveName">{{ t('common.save') }}</el-button>
                <el-button size="small" @click="renaming = false">{{ t('common.cancel') }}</el-button>
              </template>
              <template v-else>
                <span class="vault-value">{{ s.device.name }}</span>
                <el-button text size="small" @click="startRename">{{ t('vault.settings.rename') }}</el-button>
              </template>
            </div>
            <div class="vault-row">
              <span class="vault-label">{{ t('vault.fingerprint') }}</span>
              <span class="vault-value mono">{{ s.device.fingerprint }}</span>
            </div>
          </div>
          <el-button @click="onSignOut">{{ t('vault.signOut') }}</el-button>
        </div>
      </div>

      <!-- Sync -->
      <div class="vault-card">
        <div class="vault-card-header">{{ t('vault.settings.sync') }}</div>
        <div class="vault-card-body">
          <div class="vault-rows">
            <div class="vault-row">
              <span class="vault-label">{{ t('vault.settings.lastSync') }}</span>
              <span class="vault-value">{{ s.lastSyncAt ? formatChangedAt(s.lastSyncAt) : t('vault.settings.never') }}</span>
              <span v-if="s.offline" class="vault-tag warn">{{ t('vault.settings.offline') }}</span>
              <span v-else-if="s.error" class="vault-tag bad">{{ t('vault.settings.failed') }}</span>
              <span v-else-if="s.lastSyncAt" class="vault-tag ok">{{ t('vault.settings.upToDate') }}</span>
            </div>
            <div v-if="s.pendingOps > 0" class="vault-row">
              <span class="vault-label">{{ t('vault.settings.queued') }}</span>
              <span class="vault-value">{{ t('vault.settings.queuedCount', { n: s.pendingOps }) }}</span>
            </div>
            <div v-if="s.error" class="vault-row">
              <span class="vault-label">{{ t('vault.settings.reason') }}</span>
              <span class="vault-value vault-error">{{ errorText(s.error.code) }}</span>
            </div>
          </div>
          <el-button type="primary" :loading="s.syncing" @click="onSyncNow">{{ t('vault.settings.syncNow') }}</el-button>
          <span v-if="syncSummary" class="vault-muted vault-inline">{{ syncSummary }}</span>
        </div>
      </div>

      <!-- Pending join requests the user closed -->
      <div v-if="vault.visibleJoinRequests.length > 0 && !vault.joinRequest" class="vault-card">
        <div class="vault-card-body vault-row">
          <span class="vault-value">{{ t('vault.settings.pendingJoins', { n: vault.visibleJoinRequests.length }) }}</span>
          <el-button size="small" type="primary" @click="vault.unsnoozeJoins()">{{ t('vault.settings.review') }}</el-button>
        </div>
      </div>

      <!-- Devices -->
      <div class="vault-card">
        <div class="vault-card-header">{{ t('vault.settings.devices') }}</div>
        <ul class="vault-devices">
          <li v-for="d in vault.devices" :key="d.id" class="vault-device">
            <component :is="d.isRecovery ? KeyRound : Laptop" class="vault-device-icon vi-md" />
            <div class="vault-device-main">
              <div class="vault-device-name">
                <span>{{ d.isRecovery ? t('vault.settings.recoveryKey') : d.name }}</span>
                <span v-if="d.isThis" class="vault-tag ok">{{ t('vault.settings.thisOne') }}</span>
                <span v-if="!d.isRecovery && d.role !== 'admin'" class="vault-tag">{{ t(`vault.role.${d.role}`) }}</span>
              </div>
              <div class="vault-device-meta">
                <span class="mono">{{ d.fingerprint }}</span>
                <span v-if="d.isRecovery">· {{ t('vault.settings.recoveryHint') }}</span>
                <span v-else-if="d.lastSeenAt">· {{ t('vault.settings.lastSeen', { when: formatLastSeen(d.lastSeenAt, locale) }) }}</span>
              </div>
            </div>
            <el-button
              v-if="!d.isThis && !d.isRecovery && vault.isAdmin"
              size="small"
              type="danger"
              text
              @click="onRevoke(d)"
            >{{ t('vault.settings.revoke') }}</el-button>
          </li>
        </ul>
      </div>

      <!-- After revoking: what the removed device could read -->
      <div v-if="vault.lastRevoke" class="vault-card vault-exposed">
        <div class="vault-card-header">
          <span>{{ t('vault.revoke.exposedTitle') }}</span>
          <el-button text size="small" @click="vault.lastRevoke = null">{{ t('vault.close') }}</el-button>
        </div>
        <div class="vault-card-body">
          <p class="vault-muted">{{ t('vault.revoke.exposedDesc') }}</p>
          <ul v-if="vault.lastRevoke.exposed.length" class="vault-exposed-list">
            <li v-for="e in vault.lastRevoke.exposed" :key="e.connectionId">
              <span>{{ e.name }}</span><span class="mono vault-muted">{{ e.host }}</span>
            </li>
          </ul>
          <p v-else class="vault-muted">{{ t('vault.revoke.exposedNone') }}</p>
        </div>
      </div>

      <p v-if="vault.lastError" class="vault-error">{{ errorText(vault.lastError) }}</p>
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { ElMessageBox } from 'element-plus'
import { Cloud, KeyRound, Laptop, ShieldAlert, ShieldOff } from '@lucide/vue'
import { useI18n, locale } from '../i18n'
import { useVaultStore } from '../stores/vaultStore'
import type { VaultDevice } from '../types/vault'
import { formatChangedAt, formatLastSeen } from '../utils/vaultFormat'
import { vaultErrorText } from '../utils/vaultErrors'

const { t } = useI18n()
const vault = useVaultStore()
const s = computed(() => vault.status)
const errorText = (c: string) => vaultErrorText(t, c)
const inSetup = computed(() => ['signingIn', 'creating', 'showRecovery', 'awaitingApproval'].includes(s.value.phase))

const renaming = ref(false)
const newName = ref('')
function startRename() {
  newName.value = s.value.device.name
  renaming.value = true
}
async function saveName() {
  const n = newName.value.trim()
  if (!n) return
  if (await vault.renameThisDevice(n)) renaming.value = false
}

const syncSummary = ref('')
async function onSyncNow() {
  syncSummary.value = ''
  const r = await vault.syncNow()
  if (r.ok && r.value) syncSummary.value = t('vault.settings.syncResult', { pulled: r.value.pulled, pushed: r.value.pushed })
}

async function onRevoke(d: VaultDevice) {
  try {
    await ElMessageBox.confirm(t('vault.revoke.confirm', { name: d.name }), t('vault.revoke.title'), {
      type: 'warning',
      confirmButtonText: t('vault.settings.revoke'),
      cancelButtonText: t('common.cancel'),
      confirmButtonClass: 'el-button--danger',
    })
  } catch {
    return
  }
  await vault.revokeDevice(d.id)
}

async function onSignOut() {
  try {
    await ElMessageBox.confirm(t('vault.signOutConfirm'), t('vault.signOut'), {
      confirmButtonText: t('vault.signOut'),
      cancelButtonText: t('common.cancel'),
    })
  } catch {
    return
  }
  await vault.signOut()
}
</script>

<style scoped>
.section-title { font-size: 1.125rem; font-weight: 600; font-family: var(--font-ui); margin: 1.25rem 0; color: var(--text-primary); }
.section-desc { font-size: 0.8125rem; color: var(--text-secondary); margin: 0 0 1rem; line-height: 1.5; }
.vault-test-band {
  margin-bottom: 1rem;
  padding: 0.375rem 0.75rem;
  border-radius: var(--radius-sm);
  background: var(--warning-subtle);
  color: var(--warning);
  font-size: 0.75rem;
  font-weight: 600;
}
.vault-card {
  background: var(--bg-surface);
  border: 1px solid var(--border-subtle);
  border-radius: var(--radius-md);
  margin-bottom: 1rem;
  overflow: hidden;
}
.vault-card-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  min-height: 2.25rem;
  font-size: 0.8125rem;
  font-weight: 600;
  color: var(--text-primary);
  padding: 0.5rem 0.75rem 0.5rem 1.125rem;
  border-bottom: 1px solid var(--border-subtle);
}
.vault-card-body { padding: 1rem 1.125rem; }
.vault-empty { display: flex; flex-direction: column; align-items: center; gap: 0.75rem; text-align: center; padding: 1.75rem 1.125rem; }
.vault-empty-icon { color: var(--accent); }
.vault-rows { display: flex; flex-direction: column; gap: 0.5rem; margin-bottom: 0.875rem; }
.vault-row { display: flex; align-items: center; gap: 0.5rem 0.75rem; font-size: 0.8125rem; flex-wrap: wrap; }
.vault-label { color: var(--text-muted); min-width: 7rem; flex-shrink: 0; }
.vault-value { color: var(--text-primary); overflow-wrap: anywhere; }
.vault-rename { width: 14rem; }
.mono { font-family: var(--font-mono); }
.vault-muted { margin: 0; color: var(--text-secondary); font-size: 0.8125rem; line-height: 1.5; }
.vault-inline { margin-left: 0.75rem; }
.vault-gap { margin: 0.75rem 0; }
.vault-error { margin: 0; color: var(--error); font-size: 0.8125rem; }
.vault-tag {
  font-size: 0.6875rem;
  padding: 0.0625rem 0.5rem;
  border-radius: 0.625rem;
  font-weight: 500;
  background: var(--bg-overlay);
  color: var(--text-secondary);
}
.vault-tag.ok { background: var(--success-glow); color: var(--success); }
.vault-tag.warn { background: var(--warning-subtle); color: var(--warning); }
.vault-tag.bad { background: var(--error-glow); color: var(--error); }
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

.vault-devices { list-style: none; margin: 0; padding: 0; }
.vault-device { display: flex; align-items: center; gap: 0.75rem; padding: 0.625rem 1.125rem; }
.vault-device + .vault-device { border-top: 1px solid var(--border-subtle); }
.vault-device-icon { color: var(--text-muted); flex-shrink: 0; }
.vault-device-main { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 0.125rem; }
.vault-device-name { display: flex; align-items: center; gap: 0.5rem; font-size: 0.8125rem; color: var(--text-primary); overflow-wrap: anywhere; }
.vault-device-meta { display: flex; gap: 0.375rem; flex-wrap: wrap; font-size: 0.75rem; color: var(--text-muted); }

.vault-exposed { border-color: var(--warning); }
.vault-exposed-list { margin: 0.625rem 0 0; padding: 0; list-style: none; display: flex; flex-direction: column; gap: 0.25rem; font-size: 0.8125rem; color: var(--text-primary); }
.vault-exposed-list li { display: flex; gap: 0.75rem; }
/* Icon sizes in rem so they follow the UI scale. */
.vi-lg { width: 1.125rem; height: 1.125rem; }
.vi-md { width: 1rem; height: 1rem; }
.vi-xl { width: 1.75rem; height: 1.75rem; }
</style>

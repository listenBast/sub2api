<template>
  <AppLayout>
    <div class="space-y-6">
      <AdminTeamPicker v-model="teamId" :initial-team-id="initialTeamId" @change="onTeamChange" />

      <p v-if="!teamId" class="py-12 text-center text-sm text-gray-500 dark:text-gray-400">{{ t('team.noTeamSelected') }}</p>

      <template v-else>
        <section class="flex flex-col gap-1 border-b border-gray-200 pb-4 dark:border-dark-700">
          <h2 class="text-lg font-semibold text-gray-900 dark:text-white" data-testid="team-members-title">{{ detail?.name || '-' }}</h2>
          <p class="break-all text-sm text-gray-500 dark:text-gray-400">{{ detail?.owner.email }}</p>
        </section>

        <div class="hidden overflow-x-auto border-y border-gray-200 dark:border-dark-700 md:block">
          <table class="data-table min-w-[880px]">
            <thead>
              <tr>
                <th>{{ t('team.members') }}</th>
                <th>{{ t('team.role') }}</th>
                <th>{{ t('team.status') }}</th>
                <th>{{ t('team.personalBalance') }}</th>
                <th>{{ t('team.teamQuota') }}</th>
                <th class="text-right">{{ t('team.actions') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-if="detail" data-testid="team-member-owner-row">
                <td><p class="font-medium text-gray-900 dark:text-white">{{ detail.owner.username || detail.owner.email }}</p><p class="text-xs text-gray-500">{{ detail.owner.email }}</p></td>
                <td><TeamRoleBadge role="owner" /></td>
                <td><span class="status-badge status-active">{{ t('team.statusActive') }}</span></td>
                <td class="tabular-nums">{{ formatMoney(detail.owner.balance) }}</td>
                <td class="tabular-nums">-</td>
                <td></td>
              </tr>
              <tr v-for="member in members" :key="member.membership_id" data-testid="team-member-row">
                <td><p class="font-medium text-gray-900 dark:text-white">{{ memberDisplayName(member) }}</p><p class="text-xs text-gray-500">{{ member.email }}</p></td>
                <td><TeamRoleBadge :role="member.is_admin ? 'admin' : 'member'" /></td>
                <td><span class="status-badge status-muted">{{ membershipStatusLabel(member.status) }}</span></td>
                <td class="tabular-nums">{{ formatMoney(member.balance) }}</td>
                <td class="tabular-nums">{{ formatMoney(member.team_balance) }}</td>
                <td class="text-right">
                  <button
                    v-if="member.status === 'active'"
                    type="button"
                    class="btn"
                    :class="member.is_admin ? 'btn-secondary' : 'btn-primary'"
                    :disabled="submitting"
                    data-testid="team-member-toggle-admin"
                    @click="askToggle(member)"
                  >
                    {{ member.is_admin ? t('team.unsetAdmin') : t('team.setAdmin') }}
                  </button>
                </td>
              </tr>
              <tr v-if="loading"><td colspan="6" class="py-10 text-center text-gray-500">...</td></tr>
              <tr v-else-if="!members.length"><td colspan="6" class="py-10 text-center text-gray-500">{{ t('team.empty') }}</td></tr>
            </tbody>
          </table>
        </div>

        <div class="space-y-3 md:hidden">
          <article v-if="detail" class="rounded-md border border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-800">
            <div class="flex items-start justify-between gap-3">
              <div class="min-w-0"><p class="break-words font-medium text-gray-900 dark:text-white">{{ detail.owner.username || detail.owner.email }}</p><p class="mt-1 break-all text-xs text-gray-500 dark:text-gray-400">{{ detail.owner.email }}</p></div>
              <TeamRoleBadge role="owner" />
            </div>
          </article>
          <article v-for="member in members" :key="member.membership_id" class="rounded-md border border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-800">
            <div class="flex items-start justify-between gap-3">
              <div class="min-w-0"><p class="break-words font-medium text-gray-900 dark:text-white">{{ memberDisplayName(member) }}</p><p class="mt-1 break-all text-xs text-gray-500 dark:text-gray-400">{{ member.email }}</p></div>
              <TeamRoleBadge :role="member.is_admin ? 'admin' : 'member'" />
            </div>
            <button
              v-if="member.status === 'active'"
              type="button"
              class="btn mt-4 w-full"
              :class="member.is_admin ? 'btn-secondary' : 'btn-primary'"
              :disabled="submitting"
              @click="askToggle(member)"
            >
              {{ member.is_admin ? t('team.unsetAdmin') : t('team.setAdmin') }}
            </button>
          </article>
        </div>
      </template>

      <ConfirmDialog
        :show="confirmOpen"
        :title="selected?.is_admin ? t('team.unsetAdmin') : t('team.setAdmin')"
        :message="confirmMessage"
        @cancel="confirmOpen = false"
        @confirm="confirmToggle"
      />
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { adminTeamsAPI, type TeamMember, type TeamSummary } from '@/api/team'
import { useAppStore } from '@/stores'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import AppLayout from '@/components/layout/AppLayout.vue'
import AdminTeamPicker from '@/components/team/AdminTeamPicker.vue'
import TeamRoleBadge from '@/components/team/TeamRoleBadge.vue'

const { t, locale } = useI18n()
const route = useRoute()
const appStore = useAppStore()
const teamId = ref<number | null>(null)
const detail = ref<TeamSummary | null>(null)
const loading = ref(false)
const submitting = ref(false)
const confirmOpen = ref(false)
const selected = ref<TeamMember | null>(null)
let loadSeq = 0

const initialTeamId = computed(() => {
  const raw = Number(route.query.team_id)
  return Number.isFinite(raw) && raw > 0 ? raw : null
})
const members = computed(() => detail.value?.members ?? [])
const confirmMessage = computed(() => {
  if (!selected.value) return ''
  const member = memberDisplayName(selected.value)
  return selected.value.is_admin ? t('team.confirmUnsetAdmin', { member }) : t('team.confirmSetAdmin', { member })
})

async function onTeamChange() {
  await loadDetail()
}

async function loadDetail() {
  detail.value = null
  if (!teamId.value) return
  const current = ++loadSeq
  loading.value = true
  try {
    const item = await adminTeamsAPI.get(teamId.value)
    if (current !== loadSeq) return
    detail.value = { ...item, members: item.members ?? [] }
  } catch (error) {
    if (current === loadSeq) showError(error)
  } finally {
    if (current === loadSeq) loading.value = false
  }
}

function askToggle(member: TeamMember) {
  selected.value = member
  confirmOpen.value = true
}

async function confirmToggle() {
  confirmOpen.value = false
  if (!teamId.value || !selected.value) return
  submitting.value = true
  const enable = !selected.value.is_admin
  try {
    await adminTeamsAPI.setMemberAdmin(teamId.value, selected.value.user_id, enable)
    appStore.showSuccess(enable ? t('team.adminGranted') : t('team.adminRevoked'))
    await loadDetail()
  } catch (error) {
    showError(error)
  } finally {
    submitting.value = false
  }
}

function membershipStatusLabel(status: string) { return status === 'invited' ? t('team.statusInvited') : status === 'exit_pending' ? t('team.statusExitPending') : t('team.statusActive') }
function memberDisplayName(member: TeamMember) { return member.remark?.trim() || member.username || member.email }
function formatMoney(value: number) { return new Intl.NumberFormat(locale.value, { style: 'currency', currency: 'USD', maximumFractionDigits: 4 }).format(value || 0) }
function showError(error: unknown) { appStore.showError((error as { message?: string })?.message || t('common.unknownError')) }
</script>

<style scoped>
.data-table { @apply w-full border-collapse text-left text-sm; }
.data-table th { @apply border-b border-gray-200 px-3 py-3 text-xs font-semibold uppercase text-gray-500 dark:border-dark-700 dark:text-gray-400; }
.data-table td { @apply border-b border-gray-100 px-3 py-3 text-gray-700 dark:border-dark-700 dark:text-gray-300; }
.status-badge { @apply inline-flex whitespace-nowrap rounded px-2 py-1 text-xs font-medium; }
.status-active { @apply bg-emerald-50 text-emerald-700 dark:bg-emerald-950/40 dark:text-emerald-300; }
.status-muted { @apply bg-gray-100 text-gray-700 dark:bg-dark-700 dark:text-gray-300; }
</style>

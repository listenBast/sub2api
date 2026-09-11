<template>
  <AppLayout>
    <div class="space-y-6">
      <div v-if="context?.team" class="flex justify-end">
      <span
        class="inline-flex w-fit items-center gap-2 rounded-md border px-3 py-1.5 text-sm font-medium"
        :class="context.team.status === 'active'
          ? 'border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-800 dark:bg-emerald-950/40 dark:text-emerald-300'
          : 'border-amber-200 bg-amber-50 text-amber-700 dark:border-amber-800 dark:bg-amber-950/40 dark:text-amber-300'"
      >
        <span class="h-2 w-2 rounded-full" :class="context.team.status === 'active' ? 'bg-emerald-500' : 'bg-amber-500'"></span>
        {{ context.team.status === 'active' ? t('team.statusActive') : t('team.statusSuspended') }}
      </span>
      </div>

    <div v-if="teamStore.loading && !context" class="grid gap-4 sm:grid-cols-2 lg:grid-cols-4" aria-live="polite">
      <Skeleton v-for="index in 4" :key="index" class="h-28" />
    </div>

    <section v-else-if="context?.role === 'individual'" class="max-w-2xl border-l-4 border-primary-500 bg-white p-6 shadow-sm dark:bg-dark-800">
      <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('team.enableTitle') }}</h2>
      <p class="mt-2 text-sm leading-6 text-gray-600 dark:text-gray-400">{{ t('team.enableDescription') }}</p>
      <form class="mt-6 space-y-4" @submit.prevent="enableTeam">
        <label class="block">
          <span class="mb-1.5 block text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('team.teamName') }}</span>
          <input v-model.trim="teamName" required maxlength="100" class="form-input w-full" />
        </label>
        <button type="submit" class="btn btn-primary min-h-11" :disabled="submitting">
          {{ t('team.enable') }}
        </button>
      </form>
    </section>

    <section v-else-if="context?.role === 'invited' && context.team" class="max-w-3xl border border-primary-200 bg-primary-50/50 p-6 dark:border-primary-800 dark:bg-primary-950/20">
      <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('team.invitationTitle') }}</h2>
      <p class="mt-2 text-sm text-gray-700 dark:text-gray-300">
        {{ t('team.invitationDescription', { owner: context.team.owner.email, team: context.team.name }) }}
      </p>
      <p class="mt-4 border-l-2 border-amber-500 pl-3 text-sm leading-6 text-amber-800 dark:text-amber-300">
        {{ t('team.joinRequirement') }}
      </p>
      <div class="mt-6 flex flex-wrap gap-3">
        <button class="btn btn-primary min-h-11" :disabled="submitting" @click="respondInvite(true)">{{ t('team.accept') }}</button>
        <button class="btn btn-secondary min-h-11" :disabled="submitting" @click="respondInvite(false)">{{ t('team.reject') }}</button>
      </div>
    </section>

    <template v-else-if="context?.role === 'member' && context.team">
      <div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <MetricCard :label="t('team.personalBalance')" :value="formatMoney(context.current_membership?.balance ?? 0)" />
        <MetricCard :label="t('team.teamQuota')" :value="formatMoney(context.current_membership?.team_balance ?? 0)" />
        <MetricCard :label="t('team.availableBalance')" :value="formatMoney(context.current_membership?.total_balance ?? 0)" />
        <MetricCard :label="t('team.status')" :value="membershipStatusLabel(context.membership_status)" compact />
      </div>
      <section class="border-t border-gray-200 pt-5 dark:border-dark-700">
        <div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
          <div>
            <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ context.team.name }}</h2>
            <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ context.team.owner.email }}</p>
          </div>
          <button
            v-if="context.membership_status === 'active'"
            class="btn btn-danger min-h-11"
            @click="openConfirmation('exit')"
          >
            {{ t('team.requestExit') }}
          </button>
          <span v-else class="text-sm font-medium text-amber-700 dark:text-amber-300">{{ t('team.exitPending') }}</span>
        </div>
      </section>
    </template>

    <template v-else-if="context?.role === 'owner' && context.team">
      <div class="flex flex-col gap-4 sm:flex-row sm:items-end sm:justify-between">
        <div>
          <label class="block text-xs font-medium uppercase text-gray-500 dark:text-gray-400">{{ t('team.teamName') }}</label>
          <div class="mt-1 flex items-center gap-2">
            <input v-if="renaming" v-model.trim="teamName" maxlength="100" class="form-input max-w-sm" @keyup.enter="saveTeamName" />
            <h2 v-else class="text-xl font-semibold text-gray-900 dark:text-white">{{ context.team.name }}</h2>
            <button v-if="!renaming" class="btn btn-secondary min-h-10 px-3" @click="startRename">{{ t('common.edit') }}</button>
            <button v-else class="btn btn-primary min-h-10 px-3" :disabled="submitting" @click="saveTeamName">{{ t('common.save') }}</button>
            <button v-if="!renaming" class="btn btn-danger min-h-10 gap-2 px-3" @click="openConfirmation('dissolve')">
              <Icon name="trash" size="sm" />
              {{ t('team.dissolve') }}
            </button>
          </div>
        </div>
        <div class="inline-flex w-full overflow-x-auto rounded-md border border-gray-200 bg-gray-50 p-1 dark:border-dark-600 dark:bg-dark-800 sm:w-auto" role="tablist">
          <button
            v-for="tab in tabs"
            :key="tab.key"
            type="button"
            role="tab"
            class="min-h-10 whitespace-nowrap rounded px-4 text-sm font-medium transition-colors"
            :class="activeTab === tab.key ? 'bg-white text-primary-700 shadow-sm dark:bg-dark-700 dark:text-primary-300' : 'text-gray-600 hover:text-gray-900 dark:text-gray-400 dark:hover:text-white'"
            :aria-selected="activeTab === tab.key"
            @click="selectTab(tab.key)"
          >
            {{ tab.label }}
          </button>
        </div>
      </div>

      <template v-if="activeTab === 'overview'">
        <div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
          <MetricCard :label="t('team.teamBalance')" :value="formatMoney(context.team.total_balance)" />
          <MetricCard :label="t('team.activeMembers')" :value="formatNumber(context.team.active_member_count)" />
          <MetricCard :label="t('team.pendingInvites')" :value="formatNumber(context.team.pending_invite_count)" />
          <MetricCard :label="t('team.pendingExits')" :value="formatNumber(context.team.exit_pending_count)" />
        </div>
        <section class="grid gap-4 border-y border-gray-200 py-5 text-sm dark:border-dark-700 sm:grid-cols-2 lg:grid-cols-4">
          <div><p class="text-gray-500 dark:text-gray-400">{{ t('team.ownerAccount') }}</p><p class="mt-1 break-all font-medium text-gray-900 dark:text-white">{{ context.team.owner.email }}</p></div>
          <div><p class="text-gray-500 dark:text-gray-400">{{ t('team.status') }}</p><p class="mt-1 font-medium text-gray-900 dark:text-white">{{ context.team.status === 'active' ? t('team.statusActive') : t('team.statusSuspended') }}</p></div>
          <div><p class="text-gray-500 dark:text-gray-400">{{ t('team.memberCount') }}</p><p class="mt-1 font-medium tabular-nums text-gray-900 dark:text-white">{{ formatNumber(teamMembers.length) }}</p></div>
          <div><p class="text-gray-500 dark:text-gray-400">{{ t('team.createdAt') }}</p><p class="mt-1 font-medium text-gray-900 dark:text-white">{{ formatDate(context.team.created_at) }}</p></div>
        </section>
      </template>

      <template v-else-if="activeTab === 'members'">
        <section class="flex flex-col gap-4 border-y border-gray-200 py-5 dark:border-dark-700 lg:flex-row lg:items-end lg:justify-between">
          <div>
            <h3 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('team.inviteMember') }}</h3>
          </div>
          <form class="flex w-full flex-col gap-3 sm:flex-row lg:max-w-2xl" @submit.prevent="sendInvite">
            <label class="flex-1"><span class="sr-only">{{ t('team.inviteEmail') }}</span><input v-model.trim="inviteEmail" required type="email" class="form-input w-full" :placeholder="t('team.inviteEmail')" /></label>
            <button class="btn btn-primary min-h-11" :disabled="submitting">{{ t('team.sendInvite') }}</button>
          </form>
        </section>
        <div class="hidden overflow-x-auto md:block">
          <table class="data-table min-w-[920px]">
            <thead><tr><th>{{ t('team.members') }}</th><th>{{ t('team.status') }}</th><th>{{ t('team.personalBalance') }}</th><th>{{ t('team.teamQuota') }}</th><th>{{ t('team.availableBalance') }}</th><th>{{ t('team.joinedAt') }}</th><th class="text-right">{{ t('team.actions') }}</th></tr></thead>
            <tbody>
              <tr v-for="member in teamMembers" :key="member.membership_id">
                <td><p class="font-medium text-gray-900 dark:text-white">{{ memberDisplayName(member) }}</p><p class="text-xs text-gray-500">{{ member.email }}</p></td>
                <td><span class="status-badge">{{ membershipStatusLabel(member.status) }}</span></td>
                <td class="tabular-nums">{{ formatMoney(member.balance) }}</td>
                <td class="tabular-nums">{{ formatMoney(member.team_balance) }}</td>
                <td class="tabular-nums">{{ formatMoney(member.total_balance) }}</td>
                <td>{{ formatDate(member.joined_at || member.created_at) }}</td>
                <td>
                  <TeamMemberActions
                    :member="member"
                    @edit-remark="openRemark"
                    @allocate="openAllocation"
                    @transfer="openConfirmation('transfer', $event)"
                    @review-exit="reviewMemberExit"
                    @remove="openConfirmation('remove', $event)"
                  />
                </td>
              </tr>
              <tr v-if="!teamMembers.length"><td colspan="7" class="py-10 text-center text-gray-500">{{ t('team.empty') }}</td></tr>
            </tbody>
          </table>
        </div>
        <div class="space-y-3 md:hidden">
          <article v-for="member in teamMembers" :key="member.membership_id" class="rounded-md border border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-800">
            <div class="flex items-start justify-between gap-3">
              <div class="min-w-0">
                <p class="break-words font-medium text-gray-900 dark:text-white">{{ memberDisplayName(member) }}</p>
                <p class="mt-1 break-all text-xs text-gray-500 dark:text-gray-400">{{ member.email }}</p>
              </div>
              <span class="status-badge shrink-0">{{ membershipStatusLabel(member.status) }}</span>
            </div>
            <dl class="mt-4 grid grid-cols-2 gap-3 text-sm">
              <div><dt class="text-gray-500 dark:text-gray-400">{{ t('team.personalBalance') }}</dt><dd class="mt-1 tabular-nums text-gray-900 dark:text-white">{{ formatMoney(member.balance) }}</dd></div>
              <div><dt class="text-gray-500 dark:text-gray-400">{{ t('team.teamQuota') }}</dt><dd class="mt-1 tabular-nums text-gray-900 dark:text-white">{{ formatMoney(member.team_balance) }}</dd></div>
              <div><dt class="text-gray-500 dark:text-gray-400">{{ t('team.availableBalance') }}</dt><dd class="mt-1 tabular-nums text-gray-900 dark:text-white">{{ formatMoney(member.total_balance) }}</dd></div>
              <div><dt class="text-gray-500 dark:text-gray-400">{{ t('team.joinedAt') }}</dt><dd class="mt-1 text-gray-900 dark:text-white">{{ formatDate(member.joined_at || member.created_at) }}</dd></div>
            </dl>
            <div class="mt-4 overflow-x-auto border-t border-gray-100 pt-3 dark:border-dark-700">
              <TeamMemberActions
                :member="member"
                class="justify-start"
                @edit-remark="openRemark"
                @allocate="openAllocation"
                @transfer="openConfirmation('transfer', $event)"
                @review-exit="reviewMemberExit"
                @remove="openConfirmation('remove', $event)"
              />
            </div>
          </article>
          <p v-if="!teamMembers.length" class="py-10 text-center text-sm text-gray-500">{{ t('team.empty') }}</p>
        </div>
      </template>

      <template v-else-if="activeTab === 'usage'">
        <form class="grid gap-3 border-y border-gray-200 py-4 dark:border-dark-700 sm:grid-cols-2 lg:grid-cols-5" @submit.prevent="loadUsage(1)">
          <label><span class="mb-1.5 block text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('team.members') }}</span><select v-model.number="usageFilters.member_id" class="form-input w-full"><option :value="0">{{ t('team.filterMember') }}</option><option v-for="member in usageMemberOptions" :key="member.user_id" :value="member.user_id">{{ memberDisplayName(member) }}</option></select></label>
          <label><span class="mb-1.5 block text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('team.filterModel') }}</span><input v-model.trim="usageFilters.model" class="form-input w-full" /></label>
          <label><span class="mb-1.5 block text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('team.startDate') }}</span><input v-model="usageFilters.start_date" type="date" class="form-input w-full" /></label>
          <label><span class="mb-1.5 block text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('team.endDate') }}</span><input v-model="usageFilters.end_date" type="date" class="form-input w-full" /></label>
          <button class="btn btn-primary min-h-11 self-end" :disabled="usageLoading">{{ t('common.search') }}</button>
        </form>
        <TeamUsageSummaryCards v-if="usageFilters.member_id" class="mt-4" :summary="usageSummary" />
        <div class="mt-4 hidden overflow-x-auto md:block">
          <table class="data-table min-w-[760px]"><thead><tr><th>{{ t('team.createdAt') }}</th><th>{{ t('team.members') }}</th><th>{{ t('team.filterModel') }}</th><th>{{ t('team.totalTokens') }}</th><th>{{ t('team.actualCost') }}</th></tr></thead><tbody>
            <tr v-for="item in usage.items" :key="item.id"><td>{{ formatDate(item.created_at) }}</td><td>{{ item.member_email }}</td><td>{{ item.model }}</td><td class="tabular-nums">{{ formatNumber(item.total_tokens) }}</td><td class="tabular-nums">{{ formatMoney(item.actual_cost) }}</td></tr>
            <tr v-if="usageLoading"><td colspan="5" class="py-8 text-center text-gray-500">...</td></tr>
            <tr v-else-if="!usage.items.length"><td colspan="5" class="py-8 text-center text-gray-500">{{ t('team.empty') }}</td></tr>
          </tbody></table>
        </div>
        <div class="mt-4 space-y-3 md:hidden">
          <article v-for="item in usage.items" :key="item.id" class="rounded-md border border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-800">
            <div class="flex items-start justify-between gap-3"><div class="min-w-0"><p class="break-all font-medium text-gray-900 dark:text-white">{{ item.member_email }}</p><p class="mt-1 break-words text-sm text-gray-500 dark:text-gray-400">{{ item.model }}</p></div><p class="shrink-0 text-xs text-gray-500 dark:text-gray-400">{{ formatDate(item.created_at) }}</p></div>
            <dl class="mt-4 grid grid-cols-2 gap-3 text-sm"><div><dt class="text-gray-500 dark:text-gray-400">{{ t('team.totalTokens') }}</dt><dd class="mt-1 tabular-nums text-gray-900 dark:text-white">{{ formatNumber(item.total_tokens) }}</dd></div><div><dt class="text-gray-500 dark:text-gray-400">{{ t('team.actualCost') }}</dt><dd class="mt-1 tabular-nums text-gray-900 dark:text-white">{{ formatMoney(item.actual_cost) }}</dd></div></dl>
          </article>
          <p v-if="usageLoading" class="py-8 text-center text-sm text-gray-500">...</p>
          <p v-else-if="!usage.items.length" class="py-8 text-center text-sm text-gray-500">{{ t('team.empty') }}</p>
        </div>
        <Pagination v-if="usage.pages > 1" :total="usage.total" :page="usage.page" :page-size="20" :show-page-size-selector="false" @update:page="loadUsage" />
      </template>

      <TeamLedgerPanel
        v-else
        :items="transactions"
        :loading="transactionsLoading"
        :page="transactionsPage"
        :total="transactionsTotal"
        @update:page="loadTransactions"
      />

    </template>

    <BaseDialog :show="allocationOpen" :title="t('team.allocate')" width="narrow" @close="allocationOpen = false">
      <form id="team-allocation-form" class="space-y-4" @submit.prevent="submitAllocation">
        <div class="text-sm text-gray-600 dark:text-gray-300">
          <p class="font-medium text-gray-900 dark:text-white">{{ allocationMember ? memberDisplayName(allocationMember) : '-' }}</p>
          <p class="mt-1 break-all text-xs text-gray-500 dark:text-gray-400">{{ allocationMember?.email }}</p>
          <p class="mt-2 tabular-nums">{{ t('team.personalBalance') }}：{{ formatMoney(allocationMember?.balance ?? 0) }}</p>
          <p class="mt-1 tabular-nums">{{ t('team.teamQuota') }}：{{ formatMoney(allocationMember?.team_balance ?? 0) }}</p>
          <p class="mt-2 text-xs leading-5 text-gray-500 dark:text-gray-400">{{ t('team.allocateHint') }}</p>
        </div>
        <label class="block"><span class="mb-1.5 block text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('team.amount') }}</span><input v-model.number="allocationAmount" type="number" step="0.00000001" required class="form-input w-full tabular-nums" :placeholder="t('team.adjustAmountPlaceholder')" /></label>
        <label class="block"><span class="mb-1.5 block text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('team.note') }}</span><input v-model.trim="allocationNote" maxlength="500" class="form-input w-full" /></label>
      </form>
      <template #footer><div class="flex justify-end gap-3"><button class="btn btn-secondary min-h-11" @click="allocationOpen = false">{{ t('common.cancel') }}</button><button type="submit" form="team-allocation-form" class="btn btn-primary min-h-11" :disabled="submitting">{{ t('team.confirmAdjustment') }}</button></div></template>
    </BaseDialog>

    <BaseDialog :show="remarkOpen" :title="t('team.editRemark')" width="narrow" @close="remarkOpen = false">
      <form id="team-remark-form" class="space-y-4" @submit.prevent="submitRemark">
        <div class="rounded-md border border-gray-200 bg-gray-50 p-3 text-sm dark:border-dark-700 dark:bg-dark-800">
          <p class="font-medium text-gray-900 dark:text-white">{{ remarkMember?.username || remarkMember?.email }}</p>
          <p class="mt-1 break-all text-gray-500 dark:text-gray-400">{{ remarkMember?.email }}</p>
        </div>
        <label class="block">
          <span class="mb-1.5 block text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('team.memberRemark') }}</span>
          <input v-model.trim="memberRemark" maxlength="100" class="form-input w-full" :placeholder="t('team.remarkPlaceholder')" />
        </label>
      </form>
      <template #footer><div class="flex justify-end gap-3"><button class="btn btn-secondary min-h-11" @click="remarkOpen = false">{{ t('common.cancel') }}</button><button type="submit" form="team-remark-form" class="btn btn-primary min-h-11" :disabled="submitting">{{ t('common.save') }}</button></div></template>
    </BaseDialog>

    <ConfirmDialog
      :show="confirmationOpen"
      :title="confirmationTitle"
      :message="confirmationMessage"
      danger
      @cancel="confirmationOpen = false"
      @confirm="confirmAction"
    />
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, defineComponent, h, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import teamAPI, { type TeamMember, type TeamMemberUsageSummary, type TeamOwner, type TeamTransaction, type TeamUsageItem } from '@/api/team'
import { useAppStore, useAuthStore, useTeamStore } from '@/stores'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import Pagination from '@/components/common/Pagination.vue'
import Skeleton from '@/components/common/Skeleton.vue'
import Icon from '@/components/icons/Icon.vue'
import AppLayout from '@/components/layout/AppLayout.vue'
import TeamLedgerPanel from '@/components/team/TeamLedgerPanel.vue'
import TeamMemberActions from '@/components/team/TeamMemberActions.vue'
import TeamUsageSummaryCards from '@/components/team/TeamUsageSummaryCards.vue'

const { t, locale } = useI18n()
const appStore = useAppStore()
const authStore = useAuthStore()
const teamStore = useTeamStore()
const context = computed(() => teamStore.context)
const teamMembers = computed(() => context.value?.team?.members ?? [])
const teamName = ref('')
const inviteEmail = ref('')
const submitting = ref(false)
const renaming = ref(false)
const activeTab = ref<'overview' | 'members' | 'usage' | 'ledger'>('overview')
const transactions = ref<TeamTransaction[]>([])
const transactionsPage = ref(1)
const transactionsTotal = ref(0)
const transactionsLoading = ref(false)
const usage = reactive<{ items: TeamUsageItem[]; total: number; page: number; pages: number }>({ items: [], total: 0, page: 1, pages: 1 })
const usageFilters = reactive({ member_id: 0, model: '', start_date: '', end_date: '' })
const usageLoading = ref(false)
const usageLoaded = ref(false)
const usageSummary = ref<TeamMemberUsageSummary | null>(null)
const allocationOpen = ref(false)
const allocationMember = ref<TeamMember | null>(null)
const allocationAmount = ref<number | null>(null)
const allocationNote = ref('')
const remarkOpen = ref(false)
const remarkMember = ref<TeamMember | null>(null)
const memberRemark = ref('')
const confirmationOpen = ref(false)
const confirmationType = ref<'exit' | 'remove' | 'dissolve' | 'transfer'>('exit')
const confirmationMember = ref<TeamMember | null>(null)
const usageMemberOptions = computed(() => {
  const team = context.value?.team
  if (!team) return []
  return [team.owner, ...teamMembers.value.filter((member) => member.status !== 'invited')]
})

const tabs = computed(() => [
  { key: 'overview' as const, label: t('team.overview') },
  { key: 'members' as const, label: t('team.members') },
  { key: 'usage' as const, label: t('team.usage') },
  { key: 'ledger' as const, label: t('team.ledger') }
])
const confirmationTitle = computed(() => {
  if (confirmationType.value === 'exit') return t('team.requestExit')
  if (confirmationType.value === 'dissolve') return t('team.dissolve')
  if (confirmationType.value === 'transfer') return t('team.transferOwnerTitle')
  return confirmationMember.value?.status === 'invited' ? t('team.cancelInvite') : t('team.remove')
})
const confirmationMessage = computed(() => {
  if (confirmationType.value === 'exit') return t('team.confirmExit')
  if (confirmationType.value === 'dissolve') return t('team.confirmDissolve')
  if (confirmationType.value === 'transfer') return t('team.confirmTransferOwner', { member: confirmationMember.value ? memberDisplayName(confirmationMember.value) : '-' })
  return confirmationMember.value?.status === 'invited' ? t('team.confirmCancelInvite') : t('team.confirmRemove')
})

const MetricCard = defineComponent({
  props: { label: { type: String, required: true }, value: { type: String, required: true }, compact: Boolean },
  setup(props) {
    return () => h('div', { class: 'rounded-md border border-gray-200 bg-white p-5 dark:border-dark-700 dark:bg-dark-800' }, [
      h('p', { class: 'text-sm font-medium text-gray-500 dark:text-gray-400' }, props.label),
      h('p', { class: [props.compact ? 'text-base' : 'text-2xl tabular-nums', 'mt-2 break-words font-semibold text-gray-900 dark:text-white'] }, props.value)
    ])
  }
})

onMounted(async () => {
  await refreshContext()
})

async function refreshContext() {
  try {
    const value = await teamStore.fetchContext(true)
    teamName.value = value.team?.name ?? ''
  } catch (error) {
    showError(error)
  }
}

async function runAction(action: () => Promise<unknown>, success = true) {
  submitting.value = true
  try {
    await action()
    await refreshContext()
    await authStore.refreshUser().catch(() => undefined)
    if (success) appStore.showSuccess(t('team.saved'))
  } catch (error) {
    showError(error)
  } finally {
    submitting.value = false
  }
}

async function enableTeam() { await runAction(async () => teamStore.setContext(await teamAPI.upgrade(teamName.value))) }
async function saveTeamName() { await runAction(async () => teamStore.setContext(await teamAPI.rename(teamName.value))); renaming.value = false }
function startRename() { teamName.value = context.value?.team?.name ?? ''; renaming.value = true }
async function respondInvite(accept: boolean) { await runAction(async () => teamStore.setContext(await teamAPI.respondInvitation(accept))) }
async function sendInvite() { await runAction(async () => { await teamAPI.invite(inviteEmail.value); inviteEmail.value = '' }) }

function selectTab(tab: typeof activeTab.value) {
  activeTab.value = tab
  if (tab === 'ledger') void loadTransactions(1)
  if (tab === 'usage' && !usageLoaded.value) void loadUsage(1)
}

async function loadUsage(page: number) {
  usageLoading.value = true
  // Clear the previous member's totals before applying a new selection.
  usageSummary.value = null
  try {
    const params = {
      page,
      page_size: 20,
      member_id: usageFilters.member_id || undefined,
      model: usageFilters.model || undefined,
      start_date: usageFilters.start_date || undefined,
      end_date: usageFilters.end_date || undefined
    }
    const [result, summary] = await Promise.all([
      teamAPI.listUsage(params),
      usageFilters.member_id ? teamAPI.getUsageSummary(params) : Promise.resolve(null)
    ])
    Object.assign(usage, result)
    usageSummary.value = summary
    usageLoaded.value = true
  } catch (error) {
    showError(error)
  } finally {
    usageLoading.value = false
  }
}

async function loadTransactions(page: number) {
  transactionsLoading.value = true
  try {
    const result = await teamAPI.listTransactions(page, 20)
    transactions.value = result.items
    transactionsPage.value = result.page
    transactionsTotal.value = result.total
  } catch (error) {
    showError(error)
  } finally {
    transactionsLoading.value = false
  }
}

function openAllocation(member: TeamMember) { allocationMember.value = member; allocationAmount.value = null; allocationNote.value = ''; allocationOpen.value = true }
async function submitAllocation() {
  if (!allocationMember.value || allocationAmount.value === null || !Number.isFinite(allocationAmount.value) || allocationAmount.value === 0) return
  await runAction(async () => { await teamAPI.allocateBalance(allocationMember.value!.user_id, allocationAmount.value!, allocationNote.value); allocationOpen.value = false })
}

function openRemark(member: TeamMember) {
  remarkMember.value = member
  memberRemark.value = member.remark || ''
  remarkOpen.value = true
}

async function submitRemark() {
  if (!remarkMember.value) return
  await runAction(async () => {
    await teamAPI.updateMemberRemark(remarkMember.value!.user_id, memberRemark.value)
    remarkOpen.value = false
  })
}

async function reviewMemberExit(member: TeamMember, approve: boolean) {
  await runAction(() => teamAPI.reviewExit(member.user_id, approve))
}

function openConfirmation(type: 'exit' | 'remove' | 'dissolve' | 'transfer', member?: TeamMember) { confirmationType.value = type; confirmationMember.value = member ?? null; confirmationOpen.value = true }
async function confirmAction() {
  confirmationOpen.value = false
  if (confirmationType.value === 'exit') await runAction(() => teamAPI.requestExit())
  else if (confirmationType.value === 'dissolve') await runAction(() => teamAPI.dissolve())
  else if (confirmationType.value === 'transfer' && confirmationMember.value) {
    await runAction(async () => teamStore.setContext(await teamAPI.transferOwnership(confirmationMember.value!.user_id)))
  }
  else if (confirmationMember.value) await runAction(() => teamAPI.removeMember(confirmationMember.value!.user_id))
}

function membershipStatusLabel(status?: string) {
  if (status === 'invited') return t('team.statusInvited')
  if (status === 'exit_pending') return t('team.statusExitPending')
  return t('team.statusActive')
}
function memberDisplayName(member: TeamMember | TeamOwner) {
  const remark = 'remark' in member && typeof member.remark === 'string' ? member.remark.trim() : ''
  return remark || member.username || member.email
}
function formatMoney(value: number) { return new Intl.NumberFormat(locale.value, { style: 'currency', currency: 'USD', maximumFractionDigits: 4 }).format(value || 0) }
function formatNumber(value: number) { return new Intl.NumberFormat(locale.value, { notation: value > 999999 ? 'compact' : 'standard', maximumFractionDigits: 1 }).format(value || 0) }
function formatDate(value?: string | null) { return value ? new Intl.DateTimeFormat(locale.value, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value)) : '-' }
function showError(error: unknown) { appStore.showError((error as { message?: string })?.message || t('common.unknownError')) }
</script>

<style scoped>
.form-input { @apply min-h-11 rounded-md border border-gray-300 bg-white px-3 py-2 text-sm text-gray-900 outline-none transition focus:border-primary-500 focus:ring-2 focus:ring-primary-500/20 dark:border-dark-600 dark:bg-dark-800 dark:text-white; }
.btn { @apply inline-flex items-center justify-center rounded-md px-4 py-2 text-sm font-medium outline-none transition focus:ring-2 focus:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50 dark:focus:ring-offset-dark-900; }
.btn-primary { @apply bg-primary-600 text-white hover:bg-primary-700 focus:ring-primary-500; }
.btn-secondary { @apply border border-gray-300 bg-white text-gray-700 hover:bg-gray-50 focus:ring-primary-500 dark:border-dark-600 dark:bg-dark-800 dark:text-gray-200 dark:hover:bg-dark-700; }
.btn-danger { @apply border border-red-600 bg-red-600 text-white hover:border-red-700 hover:bg-red-700 focus:ring-red-500 dark:border-red-500 dark:bg-red-600 dark:text-white dark:hover:bg-red-700; color: #fff !important; }
.data-table { @apply w-full border-collapse text-left text-sm; }
.data-table th { @apply border-b border-gray-200 px-3 py-3 text-xs font-semibold uppercase text-gray-500 dark:border-dark-700 dark:text-gray-400; }
.data-table td { @apply border-b border-gray-100 px-3 py-3 text-gray-700 dark:border-dark-700 dark:text-gray-300; }
.status-badge { @apply inline-flex rounded px-2 py-1 text-xs font-medium bg-gray-100 text-gray-700 dark:bg-dark-700 dark:text-gray-300; }
</style>

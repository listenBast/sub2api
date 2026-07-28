<template>
  <AppLayout>
    <div class="space-y-6">
    <form class="flex flex-col gap-3 border-b border-gray-200 pb-5 dark:border-dark-700 sm:flex-row" @submit.prevent="loadTeams(1)">
      <label class="flex-1"><span class="sr-only">{{ t('team.search') }}</span><input v-model.trim="filters.search" class="form-input w-full" :placeholder="t('team.search')" /></label>
      <label class="sm:w-48"><span class="sr-only">{{ t('team.status') }}</span><select v-model="filters.status" class="form-input w-full"><option value="">{{ t('team.allStatuses') }}</option><option value="active">{{ t('team.statusActive') }}</option><option value="suspended">{{ t('team.statusSuspended') }}</option></select></label>
      <button class="btn btn-primary min-h-11">{{ t('common.search') }}</button>
      <button type="button" class="btn btn-secondary min-h-11 gap-2" @click="openCreateTeam">
        <Icon name="plus" size="sm" />
        {{ t('team.createTeam') }}
      </button>
    </form>

    <div class="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
      <div class="metric"><p>{{ t('team.totalTeams') }}</p><strong>{{ formatNumber(overview.total_teams) }}</strong></div>
      <div class="metric"><p>{{ t('team.activeTeams') }}</p><strong>{{ formatNumber(overview.active_teams) }}</strong></div>
      <div class="metric"><p>{{ t('team.teamAccounts') }}</p><strong>{{ formatNumber(overview.total_teams + overview.member_count) }}</strong></div>
      <div class="metric"><p>{{ t('team.teamBalance') }}</p><strong>{{ formatMoney(overview.total_balance) }}</strong></div>
    </div>
    <dl class="grid gap-3 border-y border-gray-200 py-3 text-sm dark:border-dark-700 sm:grid-cols-3">
      <div class="flex items-center justify-between gap-3 sm:justify-start"><dt class="text-gray-500 dark:text-gray-400">{{ t('team.suspendedTeams') }}</dt><dd class="font-semibold tabular-nums text-gray-900 dark:text-white">{{ formatNumber(overview.suspended_teams) }}</dd></div>
      <div class="flex items-center justify-between gap-3 sm:justify-start"><dt class="text-gray-500 dark:text-gray-400">{{ t('team.pendingInvites') }}</dt><dd class="font-semibold tabular-nums text-gray-900 dark:text-white">{{ formatNumber(overview.pending_invites) }}</dd></div>
      <div class="flex items-center justify-between gap-3 sm:justify-start"><dt class="text-gray-500 dark:text-gray-400">{{ t('team.pendingExits') }}</dt><dd class="font-semibold tabular-nums text-gray-900 dark:text-white">{{ formatNumber(overview.exit_pending) }}</dd></div>
    </dl>

    <div class="hidden overflow-x-auto border-y border-gray-200 dark:border-dark-700 md:block">
      <table class="data-table min-w-[900px]">
        <thead><tr><th>{{ t('team.teamName') }}</th><th>{{ t('team.owner') }}</th><th>{{ t('team.status') }}</th><th>{{ t('team.members') }}</th><th>{{ t('team.teamBalance') }}</th><th>{{ t('team.createdAt') }}</th><th class="text-right">{{ t('team.actions') }}</th></tr></thead>
        <tbody>
          <tr v-for="team in teams.items" :key="team.id">
            <td class="font-medium text-gray-900 dark:text-white">{{ team.name }}</td>
            <td><p>{{ team.owner.username || team.owner.email }}</p><p class="text-xs text-gray-500">{{ team.owner.email }}</p></td>
            <td><span class="status-badge" :class="team.status === 'active' ? 'status-active' : 'status-suspended'">{{ team.status === 'active' ? t('team.statusActive') : t('team.statusSuspended') }}</span></td>
            <td class="tabular-nums">{{ team.active_member_count }}<span v-if="team.pending_invite_count" class="ml-1 text-xs text-amber-600">+{{ team.pending_invite_count }}</span></td>
            <td class="tabular-nums">{{ formatMoney(team.total_balance) }}</td><td>{{ formatDate(team.created_at) }}</td>
            <td class="text-right"><button class="btn btn-secondary min-h-10 px-3" @click="openDetails(team)">{{ t('team.details') }}</button></td>
          </tr>
          <tr v-if="!loading && !teams.items.length"><td colspan="7" class="py-12 text-center text-gray-500">{{ t('team.empty') }}</td></tr>
          <tr v-if="loading"><td colspan="7" class="py-12 text-center text-gray-500">...</td></tr>
        </tbody>
      </table>
    </div>
    <div class="space-y-3 md:hidden">
      <article v-for="team in teams.items" :key="team.id" class="rounded-md border border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-800">
        <div class="flex items-start justify-between gap-3">
          <div class="min-w-0"><h3 class="break-words font-semibold text-gray-900 dark:text-white">{{ team.name }}</h3><p class="mt-1 break-all text-xs text-gray-500 dark:text-gray-400">{{ team.owner.email }}</p></div>
          <span class="status-badge shrink-0" :class="team.status === 'active' ? 'status-active' : 'status-suspended'">{{ team.status === 'active' ? t('team.statusActive') : t('team.statusSuspended') }}</span>
        </div>
        <dl class="mt-4 grid grid-cols-2 gap-3 text-sm">
          <div><dt class="text-gray-500 dark:text-gray-400">{{ t('team.owner') }}</dt><dd class="mt-1 break-words text-gray-900 dark:text-white">{{ team.owner.username || team.owner.email }}</dd></div>
          <div><dt class="text-gray-500 dark:text-gray-400">{{ t('team.members') }}</dt><dd class="mt-1 tabular-nums text-gray-900 dark:text-white">{{ team.active_member_count }}<span v-if="team.pending_invite_count" class="ml-1 text-xs text-amber-600">+{{ team.pending_invite_count }}</span></dd></div>
          <div><dt class="text-gray-500 dark:text-gray-400">{{ t('team.teamBalance') }}</dt><dd class="mt-1 tabular-nums text-gray-900 dark:text-white">{{ formatMoney(team.total_balance) }}</dd></div>
          <div><dt class="text-gray-500 dark:text-gray-400">{{ t('team.createdAt') }}</dt><dd class="mt-1 text-gray-900 dark:text-white">{{ formatDate(team.created_at) }}</dd></div>
        </dl>
        <button class="btn btn-secondary mt-4 min-h-11 w-full px-3" @click="openDetails(team)">{{ t('team.details') }}</button>
      </article>
      <p v-if="loading" class="py-12 text-center text-sm text-gray-500">...</p>
      <p v-else-if="!teams.items.length" class="py-12 text-center text-sm text-gray-500">{{ t('team.empty') }}</p>
    </div>
    <Pagination v-if="teams.pages > 1" :total="teams.total" :page="teams.page" :page-size="20" :show-page-size-selector="false" @update:page="loadTeams" />

    <BaseDialog :show="detailOpen" :title="detail?.name || t('team.details')" width="wide" @close="detailOpen = false">
      <template v-if="detail">
        <div class="flex flex-col gap-4 border-b border-gray-200 pb-4 dark:border-dark-700 sm:flex-row sm:items-center sm:justify-between">
          <div><p class="text-sm font-medium text-gray-900 dark:text-white">{{ detail.owner.email }}</p><p class="mt-1 text-xs text-gray-500">ID {{ detail.id }}</p></div>
          <div class="flex flex-wrap items-center justify-end gap-2">
            <button type="button" class="btn btn-secondary min-h-10 gap-2" :disabled="submitting || detail.status !== 'active'" @click="openAddMember">
              <Icon name="userPlus" size="sm" />
              {{ t('team.addMember') }}
            </button>
            <button type="button" class="btn btn-danger min-h-10 gap-2" :disabled="submitting" @click="deleteTeamConfirmOpen = true">
              <Icon name="trash" size="sm" />
              {{ t('team.deleteTeam') }}
            </button>
            <button type="button" class="btn min-h-10" :class="detail.status === 'active' ? 'btn-danger' : 'btn-primary'" :disabled="submitting" @click="openStatusConfirmation">
              {{ detail.status === 'active' ? t('team.suspend') : t('team.resume') }}
            </button>
          </div>
        </div>
        <div class="mt-4 grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
          <div class="metric"><p>{{ t('team.teamBalance') }}</p><strong>{{ formatMoney(dashboard?.team_balance ?? detail.total_balance) }}</strong></div>
          <div class="metric"><p>{{ t('team.totalRequests') }}</p><strong>{{ formatNumber(dashboard?.total_requests ?? 0) }}</strong></div>
          <div class="metric"><p>{{ t('team.totalTokens') }}</p><strong>{{ formatNumber(dashboard?.total_tokens ?? 0) }}</strong></div>
          <div class="metric"><p>{{ t('team.actualCost') }}</p><strong>{{ formatMoney(dashboard?.actual_cost ?? 0) }}</strong></div>
        </div>
        <div class="mt-5 inline-flex max-w-full overflow-x-auto rounded-md border border-gray-200 bg-gray-50 p-1 dark:border-dark-600 dark:bg-dark-800" role="tablist">
          <button v-for="tab in detailTabs" :key="tab.key" class="min-h-10 whitespace-nowrap rounded px-4 text-sm font-medium" :class="detailTab === tab.key ? 'bg-white text-primary-700 shadow-sm dark:bg-dark-700 dark:text-primary-300' : 'text-gray-600 dark:text-gray-400'" @click="selectDetailTab(tab.key)">{{ tab.label }}</button>
        </div>

        <div v-if="detailTab === 'members'" class="mt-4 hidden overflow-x-auto md:block">
          <table class="data-table min-w-[760px]"><thead><tr><th>{{ t('team.members') }}</th><th>{{ t('team.status') }}</th><th>{{ t('team.balance') }}</th><th>{{ t('team.frozenBalance') }}</th><th class="text-right">{{ t('team.actions') }}</th></tr></thead><tbody>
            <tr v-for="member in detailMembers" :key="member.membership_id"><td><p class="font-medium">{{ memberDisplayName(member) }}</p><p class="text-xs text-gray-500">{{ member.email }}</p></td><td>{{ membershipStatusLabel(member.status) }}</td><td class="tabular-nums">{{ formatMoney(member.balance) }}</td><td class="tabular-nums">{{ formatMoney(member.frozen_balance) }}</td><td class="text-right"><div class="flex justify-end gap-1"><button type="button" class="icon-action" :title="t('team.editRemark')" @click="openAdminRemark(member)"><Icon name="edit" size="sm" /></button><button type="button" class="icon-action icon-action-danger" :title="member.status === 'invited' ? t('team.cancelInvite') : t('team.remove')" @click="confirmRemove(member)"><Icon :name="member.status === 'invited' ? 'xCircle' : 'trash'" size="sm" /></button></div></td></tr>
            <tr v-if="!detailMembers.length"><td colspan="5" class="py-8 text-center text-gray-500">{{ t('team.empty') }}</td></tr>
          </tbody></table>
        </div>
        <div v-if="detailTab === 'members'" class="mt-4 space-y-3 md:hidden">
          <article v-for="member in detailMembers" :key="member.membership_id" class="rounded-md border border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-800">
            <div class="flex items-start justify-between gap-3">
              <div class="min-w-0"><p class="break-words font-medium text-gray-900 dark:text-white">{{ memberDisplayName(member) }}</p><p class="mt-1 break-all text-xs text-gray-500 dark:text-gray-400">{{ member.email }}</p></div>
              <span class="status-badge shrink-0">{{ membershipStatusLabel(member.status) }}</span>
            </div>
            <dl class="mt-4 grid grid-cols-2 gap-3 text-sm">
              <div><dt class="text-gray-500 dark:text-gray-400">{{ t('team.balance') }}</dt><dd class="mt-1 tabular-nums text-gray-900 dark:text-white">{{ formatMoney(member.balance) }}</dd></div>
              <div><dt class="text-gray-500 dark:text-gray-400">{{ t('team.frozenBalance') }}</dt><dd class="mt-1 tabular-nums text-gray-900 dark:text-white">{{ formatMoney(member.frozen_balance) }}</dd></div>
            </dl>
            <div class="mt-4 grid grid-cols-2 gap-2 border-t border-gray-100 pt-3 dark:border-dark-700"><button type="button" class="btn btn-secondary min-h-11 gap-2" @click="openAdminRemark(member)"><Icon name="edit" size="sm" />{{ t('team.editRemark') }}</button><button type="button" class="btn btn-danger min-h-11 gap-2" @click="confirmRemove(member)"><Icon :name="member.status === 'invited' ? 'xCircle' : 'trash'" size="sm" />{{ member.status === 'invited' ? t('team.cancelInvite') : t('team.remove') }}</button></div>
          </article>
          <p v-if="!detailMembers.length" class="py-8 text-center text-sm text-gray-500">{{ t('team.empty') }}</p>
        </div>

        <form v-if="detailTab === 'usage'" class="mt-4 grid gap-3 border-y border-gray-200 py-4 dark:border-dark-700 sm:grid-cols-2 lg:grid-cols-5" @submit.prevent="loadAdminUsage(1)">
          <label><span class="mb-1.5 block text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('team.members') }}</span><select v-model.number="usageFilters.member_id" class="form-input w-full"><option :value="0">{{ t('team.filterMember') }}</option><option v-for="member in usageMemberOptions" :key="member.user_id" :value="member.user_id">{{ memberDisplayName(member) }}</option></select></label>
          <label><span class="mb-1.5 block text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('team.filterModel') }}</span><input v-model.trim="usageFilters.model" class="form-input w-full" /></label>
          <label><span class="mb-1.5 block text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('team.startDate') }}</span><input v-model="usageFilters.start_date" type="date" class="form-input w-full" /></label>
          <label><span class="mb-1.5 block text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('team.endDate') }}</span><input v-model="usageFilters.end_date" type="date" class="form-input w-full" /></label>
          <button class="btn btn-primary min-h-11 self-end" :disabled="usageLoading">{{ t('common.search') }}</button>
        </form>

        <div v-if="detailTab === 'usage'" class="mt-4 hidden overflow-x-auto md:block">
          <table class="data-table min-w-[760px]"><thead><tr><th>{{ t('team.createdAt') }}</th><th>{{ t('team.members') }}</th><th>{{ t('team.filterModel') }}</th><th>{{ t('team.totalTokens') }}</th><th>{{ t('team.actualCost') }}</th></tr></thead><tbody>
            <tr v-for="item in usage.items" :key="item.id"><td>{{ formatDate(item.created_at) }}</td><td>{{ item.member_email }}</td><td>{{ item.model }}</td><td class="tabular-nums">{{ formatNumber(item.total_tokens) }}</td><td class="tabular-nums">{{ formatMoney(item.actual_cost) }}</td></tr>
            <tr v-if="usageLoading"><td colspan="5" class="py-8 text-center text-gray-500">...</td></tr>
            <tr v-else-if="!usage.items.length"><td colspan="5" class="py-8 text-center text-gray-500">{{ t('team.empty') }}</td></tr>
          </tbody></table>
        </div>
        <div v-if="detailTab === 'usage'" class="mt-4 space-y-3 md:hidden">
          <article v-for="item in usage.items" :key="item.id" class="rounded-md border border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-800">
            <div class="flex items-start justify-between gap-3"><div class="min-w-0"><p class="break-all font-medium text-gray-900 dark:text-white">{{ item.member_email }}</p><p class="mt-1 break-words text-sm text-gray-500 dark:text-gray-400">{{ item.model }}</p></div><p class="shrink-0 text-xs text-gray-500 dark:text-gray-400">{{ formatDate(item.created_at) }}</p></div>
            <dl class="mt-4 grid grid-cols-2 gap-3 text-sm"><div><dt class="text-gray-500 dark:text-gray-400">{{ t('team.totalTokens') }}</dt><dd class="mt-1 tabular-nums text-gray-900 dark:text-white">{{ formatNumber(item.total_tokens) }}</dd></div><div><dt class="text-gray-500 dark:text-gray-400">{{ t('team.actualCost') }}</dt><dd class="mt-1 tabular-nums text-gray-900 dark:text-white">{{ formatMoney(item.actual_cost) }}</dd></div></dl>
          </article>
          <p v-if="usageLoading" class="py-8 text-center text-sm text-gray-500">...</p>
          <p v-else-if="!usage.items.length" class="py-8 text-center text-sm text-gray-500">{{ t('team.empty') }}</p>
        </div>
        <Pagination v-if="detailTab === 'usage' && usage.pages > 1" :total="usage.total" :page="usage.page" :page-size="20" :show-page-size-selector="false" @update:page="loadAdminUsage" />

        <TeamLedgerPanel
          v-if="detailTab === 'ledger'"
          class="mt-4"
          :items="transactions.items"
          :loading="transactionsLoading"
          :page="transactions.page"
          :total="transactions.total"
          @update:page="loadAdminTransactions"
        />
      </template>
    </BaseDialog>

    <BaseDialog :show="createTeamOpen" :title="t('team.createTeam')" width="narrow" @close="createTeamOpen = false">
      <form id="admin-team-create-form" class="space-y-4" @submit.prevent="createTeam">
        <label class="block"><span class="mb-1.5 block text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('team.teamName') }}</span><input v-model.trim="createTeamForm.name" required maxlength="100" class="form-input w-full" /></label>
        <label class="block">
          <span class="mb-1.5 block text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('team.ownerEmail') }}</span>
          <AdminUserSearchSelect :key="createUserSearchKey" v-model="createTeamForm.owner_email" :placeholder="t('team.searchUserPlaceholder')" />
          <span class="mt-1.5 block text-xs text-gray-500 dark:text-gray-400">{{ t('team.userSearchHint') }}</span>
        </label>
      </form>
      <template #footer><div class="flex justify-end gap-3"><button type="button" class="btn btn-secondary min-h-11" :disabled="submitting" @click="createTeamOpen = false">{{ t('common.cancel') }}</button><button type="submit" form="admin-team-create-form" class="btn btn-primary min-h-11" :disabled="submitting || !createTeamForm.owner_email">{{ t('team.createTeam') }}</button></div></template>
    </BaseDialog>

    <BaseDialog :show="addMemberOpen" :title="t('team.addMember')" width="narrow" @close="addMemberOpen = false">
      <form id="admin-team-add-member-form" class="space-y-4" @submit.prevent="addMember">
        <div class="rounded-md border border-amber-200 bg-amber-50 p-3 text-sm text-amber-800 dark:border-amber-900/60 dark:bg-amber-950/30 dark:text-amber-200">{{ t('team.directAddNotice') }}</div>
        <label class="block">
          <span class="mb-1.5 block text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('team.inviteEmail') }}</span>
          <AdminUserSearchSelect :key="addUserSearchKey" v-model="addMemberForm.email" :exclude-user-ids="memberSearchExcludedIds" :placeholder="t('team.searchUserPlaceholder')" />
          <span class="mt-1.5 block text-xs text-gray-500 dark:text-gray-400">{{ t('team.userSearchHint') }}</span>
        </label>
        <label class="block"><span class="mb-1.5 block text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('team.memberRemark') }}</span><input v-model.trim="addMemberForm.remark" maxlength="100" class="form-input w-full" :placeholder="t('team.remarkPlaceholder')" /></label>
      </form>
      <template #footer><div class="flex justify-end gap-3"><button type="button" class="btn btn-secondary min-h-11" :disabled="submitting" @click="addMemberOpen = false">{{ t('common.cancel') }}</button><button type="submit" form="admin-team-add-member-form" class="btn btn-primary min-h-11" :disabled="submitting || !addMemberForm.email">{{ t('team.addMember') }}</button></div></template>
    </BaseDialog>

    <BaseDialog :show="remarkOpen" :title="t('team.editRemark')" width="narrow" @close="remarkOpen = false">
      <form id="admin-team-remark-form" class="space-y-4" @submit.prevent="saveAdminRemark">
        <div class="rounded-md border border-gray-200 bg-gray-50 p-3 text-sm dark:border-dark-700 dark:bg-dark-800"><p class="font-medium text-gray-900 dark:text-white">{{ remarkMember?.username || remarkMember?.email }}</p><p class="mt-1 break-all text-gray-500 dark:text-gray-400">{{ remarkMember?.email }}</p></div>
        <label class="block"><span class="mb-1.5 block text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('team.memberRemark') }}</span><input v-model.trim="memberRemark" maxlength="100" class="form-input w-full" :placeholder="t('team.remarkPlaceholder')" /></label>
      </form>
      <template #footer><div class="flex justify-end gap-3"><button type="button" class="btn btn-secondary min-h-11" :disabled="submitting" @click="remarkOpen = false">{{ t('common.cancel') }}</button><button type="submit" form="admin-team-remark-form" class="btn btn-primary min-h-11" :disabled="submitting">{{ t('common.save') }}</button></div></template>
    </BaseDialog>

    <BaseDialog :show="statusConfirmOpen" :title="statusTarget === 'suspended' ? t('team.suspend') : t('team.resume')" width="narrow" @close="closeStatusConfirmation">
      <form id="team-status-form" class="space-y-4" @submit.prevent="toggleStatus">
        <div class="rounded-md border border-gray-200 bg-gray-50 p-3 text-sm dark:border-dark-700 dark:bg-dark-800">
          <p class="font-medium text-gray-900 dark:text-white">{{ detail?.name }}</p>
          <p class="mt-1 break-all text-gray-500 dark:text-gray-400">{{ detail?.owner.email }}</p>
        </div>
        <label class="block">
          <span class="mb-1.5 block text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('team.statusReason') }}</span>
          <textarea v-model.trim="statusReason" required maxlength="450" rows="4" class="form-input w-full resize-y" />
        </label>
      </form>
      <template #footer>
        <div class="flex justify-end gap-3">
          <button class="btn btn-secondary min-h-11" :disabled="submitting" @click="closeStatusConfirmation">{{ t('common.cancel') }}</button>
          <button type="submit" form="team-status-form" class="btn min-h-11" :class="statusTarget === 'suspended' ? 'btn-danger' : 'btn-primary'" :disabled="submitting">
            {{ statusTarget === 'suspended' ? t('team.suspend') : t('team.resume') }}
          </button>
        </div>
      </template>
    </BaseDialog>

    <ConfirmDialog :show="removeConfirmOpen" :title="selectedMember?.status === 'invited' ? t('team.cancelInvite') : t('team.remove')" :message="selectedMember?.status === 'invited' ? t('team.confirmAdminCancelInvite') : t('team.confirmAdminRemove')" danger @cancel="removeConfirmOpen = false" @confirm="removeSelectedMember" />
    <ConfirmDialog :show="deleteTeamConfirmOpen" :title="t('team.deleteTeam')" :message="t('team.confirmAdminDelete')" danger @cancel="deleteTeamConfirmOpen = false" @confirm="deleteCurrentTeam" />
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminTeamsAPI, type TeamAdminOverview, type TeamDashboard, type TeamMember, type TeamOwner, type TeamSummary, type TeamTransaction, type TeamUsageItem } from '@/api/team'
import { useAppStore } from '@/stores'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import Pagination from '@/components/common/Pagination.vue'
import AppLayout from '@/components/layout/AppLayout.vue'
import AdminUserSearchSelect from '@/components/admin/AdminUserSearchSelect.vue'
import TeamLedgerPanel from '@/components/team/TeamLedgerPanel.vue'

const { t, locale } = useI18n()
const appStore = useAppStore()
const loading = ref(false)
const submitting = ref(false)
const filters = reactive({ search: '', status: '' })
const teams = reactive<{ items: TeamSummary[]; total: number; page: number; pages: number }>({ items: [], total: 0, page: 1, pages: 1 })
const overview = reactive<TeamAdminOverview>({ total_teams: 0, active_teams: 0, suspended_teams: 0, member_count: 0, pending_invites: 0, exit_pending: 0, total_balance: 0 })
const detailOpen = ref(false)
const detail = ref<TeamSummary | null>(null)
const dashboard = ref<TeamDashboard | null>(null)
const transactions = reactive<{ items: TeamTransaction[]; total: number; page: number; pages: number }>({ items: [], total: 0, page: 1, pages: 1 })
const usage = reactive<{ items: TeamUsageItem[]; total: number; page: number; pages: number }>({ items: [], total: 0, page: 1, pages: 1 })
const usageFilters = reactive({ member_id: 0, model: '', start_date: '', end_date: '' })
const usageLoading = ref(false)
const usageLoaded = ref(false)
const transactionsLoading = ref(false)
const detailTab = ref<'members' | 'usage' | 'ledger'>('members')
const statusConfirmOpen = ref(false)
const statusReason = ref('')
const statusTarget = ref<'active' | 'suspended'>('suspended')
const removeConfirmOpen = ref(false)
const selectedMember = ref<TeamMember | null>(null)
const deleteTeamConfirmOpen = ref(false)
const createTeamOpen = ref(false)
const createTeamForm = reactive({ name: '', owner_email: '' })
const createUserSearchKey = ref(0)
const addMemberOpen = ref(false)
const addMemberForm = reactive({ email: '', remark: '' })
const addUserSearchKey = ref(0)
const remarkOpen = ref(false)
const remarkMember = ref<TeamMember | null>(null)
const memberRemark = ref('')

const detailTabs = computed(() => [
  { key: 'members' as const, label: t('team.members') },
  { key: 'usage' as const, label: t('team.usage') },
  { key: 'ledger' as const, label: t('team.ledger') }
])
const detailMembers = computed(() => detail.value?.members ?? [])
const memberSearchExcludedIds = computed(() => detail.value ? [detail.value.owner.user_id, ...detailMembers.value.map((member) => member.user_id)] : [])
const usageMemberOptions = computed(() => detail.value ? [
  detail.value.owner,
  ...detailMembers.value.filter((member) => member.status !== 'invited')
] : [])

onMounted(() => { void loadTeams(1); void loadOverview() })

async function loadOverview() {
  try { Object.assign(overview, await adminTeamsAPI.overview()) } catch (error) { showError(error) }
}

async function loadTeams(page: number) {
  loading.value = true
  try {
    const result = await adminTeamsAPI.list({ page, page_size: 20, status: filters.status || undefined, search: filters.search || undefined })
    Object.assign(teams, result)
  } catch (error) { showError(error) } finally { loading.value = false }
}

async function openDetails(team: TeamSummary) {
  detailOpen.value = true
  detailTab.value = 'members'
  detail.value = { ...team, members: team.members ?? [] }
  dashboard.value = null
  Object.assign(usage, { items: [], total: 0, page: 1, pages: 1 })
  Object.assign(usageFilters, { member_id: 0, model: '', start_date: '', end_date: '' })
  usageLoaded.value = false
  transactionsLoading.value = true
  try {
    const item = await adminTeamsAPI.get(team.id)
    detail.value = { ...item, members: item.members ?? [] }
    const [stats, ledger] = await Promise.allSettled([
      adminTeamsAPI.dashboard(team.id),
      adminTeamsAPI.transactions(team.id, 1, 20)
    ])
    if (stats.status === 'fulfilled') dashboard.value = stats.value
    else showError(stats.reason)
    if (ledger.status === 'fulfilled') Object.assign(transactions, ledger.value)
    else showError(ledger.reason)
  } catch (error) {
    showError(error)
  } finally {
    transactionsLoading.value = false
  }
}

function openCreateTeam() {
  Object.assign(createTeamForm, { name: '', owner_email: '' })
  createUserSearchKey.value += 1
  createTeamOpen.value = true
}

async function createTeam() {
  if (!createTeamForm.owner_email) return
  submitting.value = true
  try {
    const created = await adminTeamsAPI.create(createTeamForm)
    createTeamOpen.value = false
    await Promise.all([loadTeams(1), loadOverview()])
    await openDetails(created)
    appStore.showSuccess(t('team.saved'))
  } catch (error) { showError(error) } finally { submitting.value = false }
}

function openAddMember() {
  Object.assign(addMemberForm, { email: '', remark: '' })
  addUserSearchKey.value += 1
  addMemberOpen.value = true
}

async function addMember() {
  if (!detail.value || !addMemberForm.email) return
  submitting.value = true
  const current = detail.value
  try {
    await adminTeamsAPI.addMember(current.id, addMemberForm)
    addMemberOpen.value = false
    await Promise.all([openDetails(current), loadTeams(teams.page), loadOverview()])
    appStore.showSuccess(t('team.saved'))
  } catch (error) { showError(error) } finally { submitting.value = false }
}

function openAdminRemark(member: TeamMember) {
  remarkMember.value = member
  memberRemark.value = member.remark || ''
  remarkOpen.value = true
}

async function saveAdminRemark() {
  if (!detail.value || !remarkMember.value) return
  submitting.value = true
  const current = detail.value
  try {
    await adminTeamsAPI.updateMemberRemark(current.id, remarkMember.value.user_id, memberRemark.value)
    remarkOpen.value = false
    await openDetails(current)
    appStore.showSuccess(t('team.saved'))
  } catch (error) { showError(error) } finally { submitting.value = false }
}

async function selectDetailTab(tab: typeof detailTab.value) {
  detailTab.value = tab
  if (tab === 'usage' && !usageLoaded.value) void loadAdminUsage(1)
}

async function loadAdminUsage(page: number) {
  if (!detail.value) return
  usageLoading.value = true
  try {
    const result = await adminTeamsAPI.usage(detail.value.id, {
      page,
      page_size: 20,
      member_id: usageFilters.member_id || undefined,
      model: usageFilters.model || undefined,
      start_date: usageFilters.start_date || undefined,
      end_date: usageFilters.end_date || undefined
    })
    Object.assign(usage, result)
    usageLoaded.value = true
  } catch (error) { showError(error) } finally { usageLoading.value = false }
}

async function loadAdminTransactions(page: number) {
  if (!detail.value) return
  transactionsLoading.value = true
  try { Object.assign(transactions, await adminTeamsAPI.transactions(detail.value.id, page, 20)) } catch (error) { showError(error) } finally { transactionsLoading.value = false }
}

function openStatusConfirmation() {
  if (!detail.value) return
  statusTarget.value = detail.value.status === 'active' ? 'suspended' : 'active'
  statusReason.value = ''
  statusConfirmOpen.value = true
}

function closeStatusConfirmation() {
  if (submitting.value) return
  statusConfirmOpen.value = false
  statusReason.value = ''
}

async function toggleStatus() {
  if (!detail.value) return
  submitting.value = true
  try {
    detail.value = await adminTeamsAPI.setStatus(detail.value.id, statusTarget.value, statusReason.value)
    statusConfirmOpen.value = false
    statusReason.value = ''
    appStore.showSuccess(t('team.saved'))
    await loadTeams(teams.page)
    await loadOverview()
    await loadAdminTransactions(1)
  } catch (error) { showError(error) } finally { submitting.value = false }
}

function confirmRemove(member: TeamMember) { selectedMember.value = member; removeConfirmOpen.value = true }
async function removeSelectedMember() {
  removeConfirmOpen.value = false
  if (!detail.value || !selectedMember.value) return
  submitting.value = true
  try {
    await adminTeamsAPI.removeMember(detail.value.id, selectedMember.value.user_id)
    await openDetails(detail.value)
    await loadTeams(teams.page)
    await loadOverview()
    appStore.showSuccess(t('team.saved'))
  } catch (error) { showError(error) } finally { submitting.value = false }
}

async function deleteCurrentTeam() {
  deleteTeamConfirmOpen.value = false
  if (!detail.value) return
  submitting.value = true
  try {
    await adminTeamsAPI.deleteTeam(detail.value.id)
    detailOpen.value = false
    detail.value = null
    await Promise.all([loadTeams(1), loadOverview()])
    appStore.showSuccess(t('team.saved'))
  } catch (error) { showError(error) } finally { submitting.value = false }
}

function membershipStatusLabel(status: string) { return status === 'invited' ? t('team.statusInvited') : status === 'exit_pending' ? t('team.statusExitPending') : t('team.statusActive') }
function memberDisplayName(member: TeamMember | TeamOwner) {
  const remark = 'remark' in member && typeof member.remark === 'string' ? member.remark.trim() : ''
  return remark || member.username || member.email
}
function formatMoney(value: number) { return new Intl.NumberFormat(locale.value, { style: 'currency', currency: 'USD', maximumFractionDigits: 4 }).format(value || 0) }
function formatNumber(value: number) { return new Intl.NumberFormat(locale.value, { notation: value > 999999 ? 'compact' : 'standard', maximumFractionDigits: 1 }).format(value || 0) }
function formatDate(value: string) { return new Intl.DateTimeFormat(locale.value, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value)) }
function showError(error: unknown) { appStore.showError((error as { message?: string })?.message || t('common.unknownError')) }
</script>

<style scoped>
.form-input { @apply min-h-11 rounded-md border border-gray-300 bg-white px-3 py-2 text-sm text-gray-900 outline-none transition focus:border-primary-500 focus:ring-2 focus:ring-primary-500/20 dark:border-dark-600 dark:bg-dark-800 dark:text-white; }
.btn { @apply inline-flex items-center justify-center rounded-md px-4 py-2 text-sm font-medium outline-none transition focus:ring-2 focus:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50 dark:focus:ring-offset-dark-900; }
.btn-primary { @apply bg-primary-600 text-white hover:bg-primary-700 focus:ring-primary-500; }
.btn-secondary { @apply border border-gray-300 bg-white text-gray-700 hover:bg-gray-50 focus:ring-primary-500 dark:border-dark-600 dark:bg-dark-800 dark:text-gray-200 dark:hover:bg-dark-700; }
.btn-danger { @apply border border-red-600 bg-red-600 text-white hover:border-red-700 hover:bg-red-700 focus:ring-red-500 dark:border-red-500 dark:bg-red-600 dark:text-white dark:hover:bg-red-700; color: #fff !important; }
.metric { @apply rounded-md border border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-800; }
.metric p { @apply text-sm font-medium text-gray-500 dark:text-gray-400; }
.metric strong { @apply mt-2 block text-2xl font-semibold tabular-nums text-gray-900 dark:text-white; }
.data-table { @apply w-full border-collapse text-left text-sm; }
.data-table th { @apply border-b border-gray-200 px-3 py-3 text-xs font-semibold uppercase text-gray-500 dark:border-dark-700 dark:text-gray-400; }
.data-table td { @apply border-b border-gray-100 px-3 py-3 text-gray-700 dark:border-dark-700 dark:text-gray-300; }
.status-badge { @apply inline-flex rounded px-2 py-1 text-xs font-medium; }
.status-active { @apply bg-emerald-50 text-emerald-700 dark:bg-emerald-950/40 dark:text-emerald-300; }
.status-suspended { @apply bg-amber-50 text-amber-700 dark:bg-amber-950/40 dark:text-amber-300; }
.icon-action { @apply inline-flex h-9 w-9 items-center justify-center rounded-md text-gray-500 transition-colors hover:bg-primary-50 hover:text-primary-600 focus:outline-none focus:ring-2 focus:ring-primary-500/30 dark:text-gray-400 dark:hover:bg-primary-900/20 dark:hover:text-primary-300; }
.icon-action-danger { @apply hover:bg-red-50 hover:text-red-600 dark:hover:bg-red-900/20 dark:hover:text-red-300; }
</style>

<template>
  <section class="space-y-4" data-testid="team-spend-panel">
    <div class="flex flex-col gap-3 border-y border-gray-200 py-4 dark:border-dark-700 sm:flex-row sm:items-center sm:justify-between">
      <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('team.spendStatsHint') }}</p>
      <div class="flex flex-wrap items-center gap-2">
        <span class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('team.spendRange') }}:</span>
        <DateRangePicker v-model:start-date="startDate" v-model:end-date="endDate" @change="refresh" />
        <button type="button" class="btn btn-secondary min-h-10 px-3" :disabled="loading" data-testid="team-spend-refresh" @click="refresh">
          {{ t('team.spendLoad') }}
        </button>
      </div>
    </div>

    <div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
      <div class="metric"><p>{{ t('team.actualCost') }}</p><strong data-testid="team-spend-total-cost">{{ formatMoney(stats?.totals.actual_cost ?? 0) }}</strong></div>
      <div class="metric"><p>{{ t('team.totalRequests') }}</p><strong>{{ formatNumber(stats?.totals.requests ?? 0) }}</strong></div>
      <div class="metric"><p>{{ t('team.totalTokens') }}</p><strong>{{ formatNumber(stats?.totals.tokens ?? 0) }}</strong></div>
      <div class="metric"><p>{{ t('team.memberCount') }}</p><strong>{{ formatNumber(stats?.members.length ?? 0) }}</strong></div>
    </div>

    <div class="hidden overflow-x-auto border-y border-gray-200 dark:border-dark-700 md:block">
      <table class="data-table min-w-[820px]">
        <thead>
          <tr>
            <th>{{ t('team.members') }}</th>
            <th>{{ t('team.role') }}</th>
            <th class="text-right">{{ t('team.totalRequests') }}</th>
            <th class="text-right">{{ t('team.totalTokens') }}</th>
            <th class="text-right">{{ t('team.spendStandardCost') }}</th>
            <th class="text-right">{{ t('team.actualCost') }}</th>
            <th class="w-40">{{ t('team.spendShare') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="row in rows" :key="row.user_id" data-testid="team-spend-row">
            <td><p class="font-medium text-gray-900 dark:text-white">{{ displayName(row) }}</p><p class="text-xs text-gray-500">{{ row.email }}</p></td>
            <td><TeamRoleBadge :role="row.role" /></td>
            <td class="text-right tabular-nums">{{ formatNumber(row.requests) }}</td>
            <td class="text-right tabular-nums">{{ formatNumber(row.tokens) }}</td>
            <td class="text-right tabular-nums">{{ formatMoney(row.total_cost) }}</td>
            <td class="text-right font-medium tabular-nums text-gray-900 dark:text-white">{{ formatMoney(row.actual_cost) }}</td>
            <td>
              <div class="flex items-center gap-2">
                <div class="h-1.5 flex-1 overflow-hidden rounded-full bg-gray-100 dark:bg-dark-700"><div class="h-full rounded-full bg-primary-500" :style="{ width: share(row) + '%' }"></div></div>
                <span class="w-12 text-right text-xs tabular-nums text-gray-500 dark:text-gray-400">{{ share(row).toFixed(1) }}%</span>
              </div>
            </td>
          </tr>
          <tr v-if="rows.length" class="font-semibold" data-testid="team-spend-totals-row">
            <td colspan="2" class="text-gray-900 dark:text-white">{{ t('team.spendTotal') }}</td>
            <td class="text-right tabular-nums">{{ formatNumber(stats?.totals.requests ?? 0) }}</td>
            <td class="text-right tabular-nums">{{ formatNumber(stats?.totals.tokens ?? 0) }}</td>
            <td class="text-right tabular-nums">{{ formatMoney(stats?.totals.total_cost ?? 0) }}</td>
            <td class="text-right tabular-nums text-gray-900 dark:text-white">{{ formatMoney(stats?.totals.actual_cost ?? 0) }}</td>
            <td></td>
          </tr>
          <tr v-if="loading"><td colspan="7" class="py-10 text-center text-gray-500">...</td></tr>
          <tr v-else-if="!rows.length"><td colspan="7" class="py-10 text-center text-gray-500">{{ t('team.empty') }}</td></tr>
        </tbody>
      </table>
    </div>

    <div class="space-y-3 md:hidden">
      <article v-for="row in rows" :key="row.user_id" class="rounded-md border border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-800">
        <div class="flex items-start justify-between gap-3">
          <div class="min-w-0"><p class="break-words font-medium text-gray-900 dark:text-white">{{ displayName(row) }}</p><p class="mt-1 break-all text-xs text-gray-500 dark:text-gray-400">{{ row.email }}</p></div>
          <TeamRoleBadge :role="row.role" />
        </div>
        <dl class="mt-4 grid grid-cols-2 gap-3 text-sm">
          <div><dt class="text-gray-500 dark:text-gray-400">{{ t('team.actualCost') }}</dt><dd class="mt-1 font-medium tabular-nums text-gray-900 dark:text-white">{{ formatMoney(row.actual_cost) }}</dd></div>
          <div><dt class="text-gray-500 dark:text-gray-400">{{ t('team.spendShare') }}</dt><dd class="mt-1 tabular-nums text-gray-900 dark:text-white">{{ share(row).toFixed(1) }}%</dd></div>
          <div><dt class="text-gray-500 dark:text-gray-400">{{ t('team.totalRequests') }}</dt><dd class="mt-1 tabular-nums text-gray-900 dark:text-white">{{ formatNumber(row.requests) }}</dd></div>
          <div><dt class="text-gray-500 dark:text-gray-400">{{ t('team.totalTokens') }}</dt><dd class="mt-1 tabular-nums text-gray-900 dark:text-white">{{ formatNumber(row.tokens) }}</dd></div>
        </dl>
      </article>
      <p v-if="loading" class="py-10 text-center text-sm text-gray-500">...</p>
      <p v-else-if="!rows.length" class="py-10 text-center text-sm text-gray-500">{{ t('team.empty') }}</p>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { TeamMemberSpend, TeamMemberSpendStats, TeamSpendRangeParams } from '@/api/team'
import DateRangePicker from '@/components/common/DateRangePicker.vue'
import TeamRoleBadge from '@/components/team/TeamRoleBadge.vue'
import { formatDateLocalInput } from '@/utils/format'
import { buildTeamSpendRange } from '@/utils/teamSpend'

const props = defineProps<{
  /** 拉取统计的函数：用户端与平台管理端使用不同接口，由父组件注入。 */
  load: (range: TeamSpendRangeParams) => Promise<TeamMemberSpendStats>
  /** 变化时重新拉取（例如平台管理员切换团队）。 */
  reloadKey?: string | number | null
}>()

const emit = defineEmits<{
  error: [error: unknown]
}>()

const { t, locale } = useI18n()

const startDate = ref(formatDateLocalInput(new Date(Date.now() - 6 * 86400000)))
const endDate = ref(formatDateLocalInput(new Date()))
const stats = ref<TeamMemberSpendStats | null>(null)
const loading = ref(false)
let requestSeq = 0

const rows = computed(() => stats.value?.members ?? [])
const totalActual = computed(() => stats.value?.totals.actual_cost ?? 0)

async function refresh() {
  const seq = ++requestSeq
  loading.value = true
  try {
    const result = await props.load(buildTeamSpendRange(startDate.value, endDate.value))
    if (seq !== requestSeq) return
    stats.value = { ...result, members: result.members ?? [] }
  } catch (error) {
    if (seq !== requestSeq) return
    stats.value = null
    emit('error', error)
  } finally {
    if (seq === requestSeq) loading.value = false
  }
}

function share(row: TeamMemberSpend) {
  if (totalActual.value <= 0) return 0
  return Math.min(100, (row.actual_cost / totalActual.value) * 100)
}

function displayName(row: TeamMemberSpend) {
  return row.remark?.trim() || row.username || row.email
}

function formatMoney(value: number) { return new Intl.NumberFormat(locale.value, { style: 'currency', currency: 'USD', maximumFractionDigits: 4 }).format(value || 0) }
function formatNumber(value: number) { return new Intl.NumberFormat(locale.value, { notation: value > 999999 ? 'compact' : 'standard', maximumFractionDigits: 1 }).format(value || 0) }

watch(() => props.reloadKey, () => { void refresh() })
onMounted(() => { void refresh() })

defineExpose({ refresh })
</script>

<style scoped>
.btn { @apply inline-flex items-center justify-center rounded-md px-4 py-2 text-sm font-medium outline-none transition focus:ring-2 focus:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50 dark:focus:ring-offset-dark-900; }
.btn-secondary { @apply border border-gray-300 bg-white text-gray-700 hover:bg-gray-50 focus:ring-primary-500 dark:border-dark-600 dark:bg-dark-800 dark:text-gray-200 dark:hover:bg-dark-700; }
.metric { @apply rounded-md border border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-800; }
.metric p { @apply text-sm font-medium text-gray-500 dark:text-gray-400; }
.metric strong { @apply mt-2 block text-2xl font-semibold tabular-nums text-gray-900 dark:text-white; }
.data-table { @apply w-full border-collapse text-left text-sm; }
.data-table th { @apply border-b border-gray-200 px-3 py-3 text-xs font-semibold uppercase text-gray-500 dark:border-dark-700 dark:text-gray-400; }
.data-table td { @apply border-b border-gray-100 px-3 py-3 text-gray-700 dark:border-dark-700 dark:text-gray-300; }
</style>

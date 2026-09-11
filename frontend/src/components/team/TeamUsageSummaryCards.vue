<template>
  <section
    v-if="summary"
    aria-live="polite"
    data-testid="team-member-usage-summary"
    class="rounded-md border border-gray-200 bg-gray-50 p-4 dark:border-dark-700 dark:bg-dark-800/60"
  >
    <div class="flex flex-col gap-1 sm:flex-row sm:items-end sm:justify-between">
      <div>
        <h3 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('team.memberSummaryTitle') }}</h3>
        <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('team.memberSummaryHint') }}</p>
      </div>
      <p class="text-sm font-medium text-gray-700 dark:text-gray-300">
        {{ displayName }}
        <span class="ml-1 break-all text-xs font-normal text-gray-500 dark:text-gray-400">{{ summary.email }}</span>
      </p>
    </div>
    <div class="mt-4 grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
      <div class="rounded-md border border-gray-200 bg-white p-3 dark:border-dark-700 dark:bg-dark-800">
        <p class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('team.availableBalance') }}</p>
        <p class="mt-1 text-lg font-semibold tabular-nums text-gray-900 dark:text-white">{{ formatMoney(summary.total_balance) }}</p>
        <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
          {{ t('team.personalBalance') }} {{ formatMoney(summary.balance) }}  /  {{ t('team.teamQuota') }} {{ formatMoney(summary.team_balance) }}
        </p>
      </div>
      <div class="rounded-md border border-gray-200 bg-white p-3 dark:border-dark-700 dark:bg-dark-800">
        <p class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('team.totalRequests') }}</p>
        <p class="mt-1 text-lg font-semibold tabular-nums text-gray-900 dark:text-white">{{ formatNumber(summary.requests) }}</p>
      </div>
      <div class="rounded-md border border-gray-200 bg-white p-3 dark:border-dark-700 dark:bg-dark-800">
        <p class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('team.totalTokens') }}</p>
        <p class="mt-1 text-lg font-semibold tabular-nums text-gray-900 dark:text-white">{{ formatNumber(summary.tokens) }}</p>
      </div>
      <div class="rounded-md border border-gray-200 bg-white p-3 dark:border-dark-700 dark:bg-dark-800">
        <p class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('team.actualCost') }}</p>
        <p class="mt-1 text-lg font-semibold tabular-nums text-gray-900 dark:text-white">{{ formatMoney(summary.actual_cost) }}</p>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { TeamMemberUsageSummary } from '@/api/team'

const props = defineProps<{
  summary: TeamMemberUsageSummary | null
}>()

const { t, locale } = useI18n()

const displayName = computed(() => {
  const item = props.summary
  if (!item) return ''
  return item.remark?.trim() || item.username || item.email
})

function formatMoney(value: number) {
  return new Intl.NumberFormat(locale.value, { style: 'currency', currency: 'USD', maximumFractionDigits: 4 }).format(value || 0)
}

function formatNumber(value: number) {
  return new Intl.NumberFormat(locale.value, { notation: value > 999999 ? 'compact' : 'standard', maximumFractionDigits: 1 }).format(value || 0)
}
</script>

<template>
  <section class="overflow-hidden rounded-md border border-gray-200 bg-white dark:border-dark-700 dark:bg-dark-800">
    <div class="flex items-center justify-between gap-4 border-b border-gray-200 px-5 py-4 dark:border-dark-700">
      <h3 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('team.ledger') }}</h3>
      <span class="shrink-0 text-sm tabular-nums text-gray-500 dark:text-gray-400">
        {{ t('team.recordCount', { count: total }) }}
      </span>
    </div>

    <div class="hidden overflow-x-auto md:block">
      <table class="w-full min-w-[1080px] border-collapse text-left text-sm">
        <thead class="bg-gray-50/80 dark:bg-dark-900/40">
          <tr>
            <th class="border-b border-gray-200 px-5 py-3 text-xs font-semibold text-gray-500 dark:border-dark-700 dark:text-gray-400">{{ t('team.createdAt') }}</th>
            <th class="border-b border-gray-200 px-5 py-3 text-xs font-semibold text-gray-500 dark:border-dark-700 dark:text-gray-400">{{ t('team.transactionType') }}</th>
            <th class="border-b border-gray-200 px-5 py-3 text-xs font-semibold text-gray-500 dark:border-dark-700 dark:text-gray-400">{{ t('team.relatedMember') }}</th>
            <th class="border-b border-gray-200 px-5 py-3 text-right text-xs font-semibold text-gray-500 dark:border-dark-700 dark:text-gray-400">{{ t('team.amount') }}</th>
            <th class="border-b border-gray-200 px-5 py-3 text-xs font-semibold text-gray-500 dark:border-dark-700 dark:text-gray-400">{{ t('team.ownerBalance') }}</th>
            <th class="border-b border-gray-200 px-5 py-3 text-xs font-semibold text-gray-500 dark:border-dark-700 dark:text-gray-400">{{ t('team.memberBalance') }}</th>
            <th class="border-b border-gray-200 px-5 py-3 text-xs font-semibold text-gray-500 dark:border-dark-700 dark:text-gray-400">{{ t('team.note') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-if="loading">
            <td colspan="7" class="px-5 py-12 text-center text-sm text-gray-500 dark:text-gray-400">...</td>
          </tr>
          <template v-else>
            <tr v-for="item in items" :key="item.id" class="transition-colors hover:bg-gray-50/80 dark:hover:bg-dark-700/40">
              <td class="whitespace-nowrap border-b border-gray-100 px-5 py-4 text-gray-600 dark:border-dark-700 dark:text-gray-300">{{ formatDate(item.created_at) }}</td>
              <td class="border-b border-gray-100 px-5 py-4 font-medium text-gray-900 dark:border-dark-700 dark:text-white">{{ transactionActionLabel(item.action) }}</td>
              <td class="border-b border-gray-100 px-5 py-4 dark:border-dark-700">
                <p class="font-medium text-gray-900 dark:text-white">{{ memberDisplayName(item) }}</p>
                <p v-if="item.member_email && item.member_email !== memberDisplayName(item)" class="mt-0.5 break-all text-xs text-gray-500 dark:text-gray-400">{{ item.member_email }}</p>
              </td>
              <td class="whitespace-nowrap border-b border-gray-100 px-5 py-4 text-right font-medium tabular-nums dark:border-dark-700" :class="amountClass(item.amount)">{{ formatSignedMoney(item.amount) }}</td>
              <td class="border-b border-gray-100 px-5 py-4 dark:border-dark-700">
                <div class="flex min-w-max items-center gap-2 tabular-nums text-gray-700 dark:text-gray-300">
                  <span>{{ formatMoney(item.owner_balance_before) }}</span>
                  <Icon name="arrowRight" size="xs" class="text-gray-400" />
                  <span class="font-medium text-gray-900 dark:text-white">{{ formatMoney(item.owner_balance_after) }}</span>
                </div>
              </td>
              <td class="border-b border-gray-100 px-5 py-4 dark:border-dark-700">
                <div v-if="item.member_balance_before != null && item.member_balance_after != null" class="flex min-w-max items-center gap-2 tabular-nums text-gray-700 dark:text-gray-300">
                  <span>{{ formatMoney(item.member_balance_before) }}</span>
                  <Icon name="arrowRight" size="xs" class="text-gray-400" />
                  <span class="font-medium text-gray-900 dark:text-white">{{ formatMoney(item.member_balance_after) }}</span>
                </div>
                <span v-else class="text-gray-400">-</span>
              </td>
              <td class="max-w-xs border-b border-gray-100 px-5 py-4 text-gray-600 dark:border-dark-700 dark:text-gray-300">
                <span class="line-clamp-2 break-words">{{ item.note || '-' }}</span>
              </td>
            </tr>
          </template>
          <tr v-if="!loading && !items.length">
            <td colspan="7" class="px-5 py-12 text-center text-sm text-gray-500 dark:text-gray-400">{{ t('team.empty') }}</td>
          </tr>
        </tbody>
      </table>
    </div>

    <div class="divide-y divide-gray-100 md:hidden dark:divide-dark-700">
      <div v-if="loading" class="px-4 py-12 text-center text-sm text-gray-500 dark:text-gray-400">...</div>
      <template v-else>
        <article v-for="item in items" :key="item.id" class="p-4">
          <div class="flex items-start justify-between gap-3">
            <div class="min-w-0">
              <p class="break-words font-medium text-gray-900 dark:text-white">{{ transactionActionLabel(item.action) }}</p>
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ formatDate(item.created_at) }}</p>
            </div>
            <p class="shrink-0 text-sm font-semibold tabular-nums" :class="amountClass(item.amount)">{{ formatSignedMoney(item.amount) }}</p>
          </div>
          <dl class="mt-4 space-y-3 text-sm">
            <div>
              <dt class="text-gray-500 dark:text-gray-400">{{ t('team.relatedMember') }}</dt>
              <dd class="mt-1 text-gray-900 dark:text-white">
                <p class="font-medium">{{ memberDisplayName(item) }}</p>
                <p v-if="item.member_email && item.member_email !== memberDisplayName(item)" class="mt-0.5 break-all text-xs text-gray-500 dark:text-gray-400">{{ item.member_email }}</p>
              </dd>
            </div>
            <div>
              <dt class="text-gray-500 dark:text-gray-400">{{ t('team.ownerBalance') }}</dt>
              <dd class="mt-1 flex items-center gap-2 tabular-nums text-gray-900 dark:text-white">
                <span>{{ formatMoney(item.owner_balance_before) }}</span>
                <Icon name="arrowRight" size="xs" class="text-gray-400" />
                <span class="font-medium">{{ formatMoney(item.owner_balance_after) }}</span>
              </dd>
            </div>
            <div v-if="item.member_balance_before != null && item.member_balance_after != null">
              <dt class="text-gray-500 dark:text-gray-400">{{ t('team.memberBalance') }}</dt>
              <dd class="mt-1 flex items-center gap-2 tabular-nums text-gray-900 dark:text-white">
                <span>{{ formatMoney(item.member_balance_before) }}</span>
                <Icon name="arrowRight" size="xs" class="text-gray-400" />
                <span class="font-medium">{{ formatMoney(item.member_balance_after) }}</span>
              </dd>
            </div>
            <div>
              <dt class="text-gray-500 dark:text-gray-400">{{ t('team.note') }}</dt>
              <dd class="mt-1 break-words text-gray-900 dark:text-white">{{ item.note || '-' }}</dd>
            </div>
          </dl>
        </article>
      </template>
      <div v-if="!loading && !items.length" class="px-4 py-12 text-center text-sm text-gray-500 dark:text-gray-400">{{ t('team.empty') }}</div>
    </div>

    <Pagination
      v-if="total > 0"
      :page="page"
      :page-size="pageSize"
      :total="total"
      :show-page-size-selector="false"
      @update:page="$emit('update:page', $event)"
    />
  </section>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { TeamTransaction } from '@/api/team'
import Icon from '@/components/icons/Icon.vue'
import Pagination from '@/components/common/Pagination.vue'

withDefaults(defineProps<{
  items: TeamTransaction[]
  loading?: boolean
  page: number
  total: number
  pageSize?: number
}>(), {
  loading: false,
  pageSize: 20
})

defineEmits<{
  'update:page': [page: number]
}>()

const { t, locale } = useI18n()

function transactionActionLabel(action: string) {
  return t(`team.actionsMap.${action}`, action)
}

function amountClass(amount: number) {
  if (amount > 0) return 'text-emerald-600 dark:text-emerald-400'
  if (amount < 0) return 'text-red-600 dark:text-red-400'
  return 'text-gray-700 dark:text-gray-300'
}

function memberDisplayName(item: TeamTransaction) {
  return item.member_remark?.trim() || item.member_username?.trim() || item.member_email || (item.member_id ? `#${item.member_id}` : '-')
}

function formatMoney(value: number) {
  return new Intl.NumberFormat(locale.value, {
    style: 'currency',
    currency: 'USD',
    maximumFractionDigits: 4
  }).format(value || 0)
}

function formatSignedMoney(value: number) {
  return new Intl.NumberFormat(locale.value, {
    style: 'currency',
    currency: 'USD',
    maximumFractionDigits: 4,
    signDisplay: 'exceptZero'
  }).format(value || 0)
}

function formatDate(value?: string | null) {
  return value
    ? new Intl.DateTimeFormat(locale.value, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value))
    : '-'
}
</script>

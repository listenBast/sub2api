<template>
  <BaseDialog :show="show" :title="t('team.adjustOwnerBalance')" width="narrow" @close="close">
    <form v-if="team" id="admin-owner-balance-form" class="space-y-4" @submit.prevent="submit">
      <div class="rounded-md border border-gray-200 bg-gray-50 p-3 text-sm dark:border-dark-700 dark:bg-dark-800">
        <p class="font-medium text-gray-900 dark:text-white">{{ team.name }}</p>
        <p class="mt-1 break-all text-gray-500 dark:text-gray-400" data-testid="owner-balance-email">{{ team.owner.email }}</p>
        <p class="mt-3 text-xs text-gray-500 dark:text-gray-400">{{ t('team.ownerBalanceCurrent') }}</p>
        <p class="tabular-nums text-gray-900 dark:text-white" data-testid="owner-balance-current">{{ formatMoney(currentBalance) }}</p>
      </div>
      <div>
        <span class="input-label">{{ t('team.ownerBalanceOperation') }}</span>
        <div class="inline-flex max-w-full overflow-x-auto rounded-md border border-gray-200 bg-gray-50 p-1 dark:border-dark-600 dark:bg-dark-800" role="radiogroup">
          <button
            v-for="op in operations"
            :key="op.value"
            type="button"
            role="radio"
            :data-testid="`owner-balance-op-${op.value}`"
            :aria-checked="form.operation === op.value"
            class="min-h-10 whitespace-nowrap rounded px-4 text-sm font-medium"
            :class="form.operation === op.value ? 'bg-white text-primary-700 shadow-sm dark:bg-dark-700 dark:text-primary-300' : 'text-gray-600 hover:text-gray-900 dark:text-gray-400 dark:hover:text-white'"
            @click="form.operation = op.value"
          >
            {{ op.label }}
          </button>
        </div>
      </div>
      <div>
        <label class="input-label" for="owner-balance-amount">{{ t('team.ownerBalanceAmount') }}</label>
        <input id="owner-balance-amount" v-model.number="form.amount" data-testid="owner-balance-amount" type="number" step="any" min="0" required class="input tabular-nums" />
      </div>
      <div>
        <label class="input-label" for="owner-balance-notes">{{ t('team.ownerBalanceNotes') }}</label>
        <textarea id="owner-balance-notes" v-model.trim="form.notes" maxlength="500" rows="2" class="input resize-y" />
      </div>
      <dl v-if="amountValid" class="rounded-md border border-blue-200 bg-blue-50 p-3 text-sm dark:border-blue-800 dark:bg-blue-950">
        <div class="flex items-center justify-between">
          <dt class="text-gray-700 dark:text-gray-300">{{ t('team.ownerBalanceAfter') }}</dt>
          <dd class="font-semibold tabular-nums" :class="balanceAfter < 0 ? 'text-red-600 dark:text-red-400' : 'text-gray-900 dark:text-white'" data-testid="owner-balance-after">{{ formatMoney(balanceAfter) }}</dd>
        </div>
      </dl>
      <p class="text-xs leading-5 text-gray-500 dark:text-gray-400">{{ t('team.ownerBalanceHint') }}</p>
    </form>
    <template #footer>
      <div class="flex justify-end gap-3">
        <button type="button" class="btn btn-secondary" :disabled="submitting" @click="close">{{ t('common.cancel') }}</button>
        <button type="submit" form="admin-owner-balance-form" class="btn btn-primary" :disabled="submitting" data-testid="owner-balance-submit">{{ t('common.confirm') }}</button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminTeamsAPI, type TeamSummary } from '@/api/team'
import BaseDialog from '@/components/common/BaseDialog.vue'
import { useAppStore } from '@/stores'

const props = defineProps<{
  show: boolean
  team: Pick<TeamSummary, 'id' | 'name' | 'owner'> | null
}>()

const emit = defineEmits<{
  close: []
  saved: [teamId: number]
}>()

const { t, locale } = useI18n()
const appStore = useAppStore()
const submitting = ref(false)
const form = reactive<{ operation: 'add' | 'subtract'; amount: number | null; notes: string }>({ operation: 'add', amount: null, notes: '' })

const operations = computed(() => [
  { value: 'add' as const, label: t('team.ownerBalanceAdd') },
  { value: 'subtract' as const, label: t('team.ownerBalanceSubtract') }
])
const currentBalance = computed(() => props.team?.owner.balance ?? 0)
const amountValid = computed(() => typeof form.amount === 'number' && Number.isFinite(form.amount) && form.amount > 0)
const balanceAfter = computed(() => {
  const amount = form.amount ?? 0
  return roundMoney(currentBalance.value + (form.operation === 'add' ? amount : -amount))
})

watch(() => props.show, (open) => {
  if (open) Object.assign(form, { operation: 'add', amount: null, notes: '' })
})

function close() {
  if (submitting.value) return
  emit('close')
}

async function submit() {
  if (!props.team) return
  if (!amountValid.value || form.amount === null) {
    appStore.showError(t('team.ownerBalanceAmountRequired'))
    return
  }
  if (form.operation === 'subtract' && balanceAfter.value < 0) {
    appStore.showError(t('team.ownerBalanceNegative'))
    return
  }
  submitting.value = true
  const teamId = props.team.id
  try {
    await adminTeamsAPI.adjustOwnerBalance(teamId, {
      operation: form.operation,
      balance: form.amount,
      notes: form.notes || undefined
    })
    appStore.showSuccess(t('team.ownerBalanceDone'))
    emit('saved', teamId)
  } catch (error) {
    appStore.showError((error as { message?: string })?.message || t('common.unknownError'))
  } finally {
    submitting.value = false
  }
}

function roundMoney(value: number) { const rounded = Math.round(value * 1e8) / 1e8; return Object.is(rounded, -0) ? 0 : rounded }
function formatMoney(value: number) { return new Intl.NumberFormat(locale.value, { style: 'currency', currency: 'USD', maximumFractionDigits: 4 }).format(value || 0) }
</script>

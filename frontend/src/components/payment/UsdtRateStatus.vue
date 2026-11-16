<template>
  <section class="space-y-2 rounded-lg bg-blue-50 p-3 text-sm dark:bg-blue-950" data-test="usdt-rate-status">
    <a href="https://www.coingecko.com/" target="_blank" rel="noopener noreferrer">{{ t('admin.settings.payment.usdtRateAuto') }} · CoinGecko</a>
    <p>{{ t('admin.settings.payment.usdtAutoHint') }}</p>
    <p role="status">{{ available ? t('admin.settings.payment.usdtRateAvailable') : t('admin.settings.payment.usdtRateUnavailable') }}</p>
    <template v-if="available && quote">
      <p>1 USDT = {{ quote.rate }} CNY</p>
      <p>{{ t('admin.settings.payment.usdtQuoteTime') }}: {{ new Date(quote.quoted_at).toLocaleString() }}</p>
      <p>{{ t('admin.settings.payment.usdtFetchTime') }}: {{ new Date(quote.fetched_at).toLocaleString() }}</p>
    </template>
    <button type="button" class="btn btn-secondary text-xs" :disabled="loading" @click="load">{{ t('common.refresh') }}</button>
  </section>
</template>
<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminPaymentAPI } from '@/api/admin/payment'
const { t } = useI18n()
const quote = ref<{ rate: string; quoted_at: string; fetched_at: string }>()
const ok = ref(false)
const loading = ref(false)
const now = ref(Date.now())
const available = computed(() => ok.value && quote.value && now.value - Date.parse(quote.value.quoted_at) <= 120_000 && now.value - Date.parse(quote.value.fetched_at) <= 120_000)
let timer: ReturnType<typeof setInterval> | undefined
async function load() {
  loading.value = true
  ok.value = false
  try {
    const { data } = await adminPaymentAPI.getUSDTRate()
    quote.value = data.quote
    ok.value = data.available
    now.value = Date.now()
  } catch { quote.value = undefined } finally { loading.value = false }
}
onMounted(() => { void load(); timer = setInterval(() => { now.value = Date.now() }, 1000) })
onUnmounted(() => { if (timer) clearInterval(timer) })
</script>

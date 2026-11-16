<template>
  <section class="card space-y-5 p-6" data-test="usdt-instructions">
    <h2 class="text-center text-lg font-semibold text-gray-900 dark:text-white">{{ t('payment.usdtScanTitle') }}</h2>
    <p class="rounded-lg bg-amber-50 p-3 text-sm font-semibold text-amber-900 dark:bg-amber-950 dark:text-amber-100" role="note">
      {{ t('payment.usdtNetworkWarning') }}
    </p>
    <dl class="space-y-4 text-sm">
      <div>
        <dt class="text-gray-500 dark:text-gray-400">{{ t('payment.usdtNetwork') }}</dt>
        <dd class="mt-1 text-lg font-bold text-gray-900 dark:text-white" data-test="usdt-network">{{ t(`payment.methods.${details.method}`) }}</dd>
      </div>
      <div>
        <dt class="text-gray-500 dark:text-gray-400">{{ t('payment.usdtExactAmount') }}</dt>
        <dd class="mt-1 flex flex-wrap items-center gap-3">
          <strong class="break-all text-2xl tabular-nums text-gray-900 dark:text-white" data-test="usdt-amount">{{ details.amount }} USDT</strong>
          <button type="button" class="btn btn-secondary text-xs" data-test="copy-usdt-amount" @click="copyToClipboard(details.amount)">{{ t('payment.usdtCopyAmount') }}</button>
        </dd>
      </div>
      <div>
        <dt class="text-gray-500 dark:text-gray-400">{{ t('payment.usdtAddress') }}</dt>
        <dd class="mt-1 space-y-2">
          <code class="block break-all rounded-lg bg-gray-100 p-3 text-gray-900 dark:bg-dark-800 dark:text-white" data-test="usdt-address">{{ details.address }}</code>
          <button type="button" class="btn btn-secondary text-xs" data-test="copy-usdt-address" @click="copyToClipboard(details.address)">{{ t('payment.usdtCopyAddress') }}</button>
        </dd>
      </div>
    </dl>
    <div class="flex justify-center rounded-lg bg-white p-3">
      <canvas ref="canvas" class="max-w-full" :aria-label="t('payment.usdtAddressQr')" role="img"></canvas>
    </div>
    <p v-if="qrFailed" class="text-sm text-red-600" role="alert">{{ t('payment.usdtQrFailed') }}</p>
    <p class="text-sm leading-relaxed text-gray-600 dark:text-gray-300">{{ t('payment.usdtQrHint') }}</p>
  </section>
</template>

<script setup lang="ts">
import { nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import QRCode from 'qrcode'
import { useClipboard } from '@/composables/useClipboard'
import type { CryptoPaymentDetails } from '@/types/payment'

const props = defineProps<{ details: CryptoPaymentDetails }>()
const { t } = useI18n()
const { copyToClipboard } = useClipboard()
const canvas = ref<HTMLCanvasElement | null>(null)
const qrFailed = ref(false)

watch(() => props.details.address, async (address) => {
  await nextTick()
  if (!canvas.value) return
  qrFailed.value = false
  const target = canvas.value
  try {
    // Intentionally address-only, like BEpusdt's official cashier. Never imply
    // that an exchange/wallet will infer a network, token or quantity from it.
    await QRCode.toCanvas(target, address, { width: 240, margin: 4, errorCorrectionLevel: 'M' })
    target.style.height = 'auto' // Preserve the square aspect ratio when narrow screens shrink the canvas.
  } catch {
    qrFailed.value = true
  }
}, { immediate: true })
</script>

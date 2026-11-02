<template>
  <div class="flex flex-col gap-3 border-b border-gray-200 pb-5 dark:border-dark-700 sm:flex-row sm:items-end" data-testid="admin-team-picker">
    <label class="sm:w-72">
      <span class="mb-1.5 block text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('team.search') }}</span>
      <input v-model.trim="search" class="form-input w-full" :placeholder="t('team.searchTeamPlaceholder')" data-testid="admin-team-picker-search" @input="scheduleSearch" />
    </label>
    <label class="min-w-0 flex-1">
      <span class="mb-1.5 block text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('team.selectTeam') }}</span>
      <select :value="modelValue ?? ''" class="form-input w-full" data-testid="admin-team-picker-select" @change="onSelect">
        <option value="" disabled>{{ options.length || loading ? t('team.selectTeamPlaceholder') : t('team.noTeamsFound') }}</option>
        <option v-for="team in options" :key="team.id" :value="team.id">{{ team.name }} · {{ team.owner.email }}</option>
      </select>
    </label>
  </div>
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminTeamsAPI, type TeamSummary } from '@/api/team'
import { useAppStore } from '@/stores'

const props = defineProps<{
  modelValue: number | null
  /** 首次加载完成后希望预选的团队（例如来自路由 query）。 */
  initialTeamId?: number | null
}>()

const emit = defineEmits<{
  'update:modelValue': [teamId: number | null]
  change: [team: TeamSummary | null]
}>()

const { t } = useI18n()
const appStore = useAppStore()
const search = ref('')
const options = ref<TeamSummary[]>([])
const loading = ref(false)
let timer: ReturnType<typeof setTimeout> | null = null
let seq = 0

async function load() {
  const current = ++seq
  loading.value = true
  try {
    const result = await adminTeamsAPI.list({ page: 1, page_size: 50, search: search.value || undefined })
    if (current !== seq) return
    options.value = result.items
    if (props.modelValue == null) {
      const preferred = props.initialTeamId ? result.items.find((team) => team.id === props.initialTeamId) : undefined
      const first = preferred ?? (search.value ? undefined : result.items[0])
      if (first) select(first)
    }
  } catch (error) {
    if (current === seq) appStore.showError((error as { message?: string })?.message || t('common.unknownError'))
  } finally {
    if (current === seq) loading.value = false
  }
}

function select(team: TeamSummary | null) {
  emit('update:modelValue', team?.id ?? null)
  emit('change', team)
}

function onSelect(event: Event) {
  const id = Number((event.target as HTMLSelectElement).value)
  select(options.value.find((team) => team.id === id) ?? null)
}

function scheduleSearch() {
  if (timer) clearTimeout(timer)
  timer = setTimeout(() => { void load() }, 300)
}

onMounted(() => { void load() })
onBeforeUnmount(() => { if (timer) clearTimeout(timer) })
</script>

<style scoped>
.form-input { @apply min-h-11 rounded-md border border-gray-300 bg-white px-3 py-2 text-sm text-gray-900 outline-none transition focus:border-primary-500 focus:ring-2 focus:ring-primary-500/20 dark:border-dark-600 dark:bg-dark-800 dark:text-white; }
</style>

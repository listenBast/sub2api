<template>
  <div class="grid items-end gap-3 border-b border-gray-200 pb-5 dark:border-dark-700 sm:grid-cols-[18rem_minmax(0,1fr)]" data-testid="admin-team-picker">
    <div>
      <label class="input-label" for="admin-team-picker-search">{{ t('team.search') }}</label>
      <input id="admin-team-picker-search" v-model.trim="search" class="input" :placeholder="t('team.searchTeamPlaceholder')" data-testid="admin-team-picker-search" @input="scheduleSearch" />
    </div>
    <div class="min-w-0" data-testid="admin-team-picker-select">
      <label class="input-label" for="admin-team-picker-trigger">{{ t('team.selectTeam') }}</label>
      <Select
        id="admin-team-picker-trigger"
        :model-value="modelValue"
        :options="selectOptions"
        :placeholder="options.length || loading ? t('team.selectTeamPlaceholder') : t('team.noTeamsFound')"
        :empty-text="t('team.noTeamsFound')"
        @change="onSelect"
      />
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminTeamsAPI, type TeamSummary } from '@/api/team'
import { useAppStore } from '@/stores'
import Select from '@/components/common/Select.vue'

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
const selectOptions = computed(() => options.value.map((team) => ({ value: team.id, label: `${team.name} · ${team.owner.email}` })))
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

function onSelect(value: string | number | boolean | null) {
  const id = Number(value)
  select(options.value.find((team) => team.id === id) ?? null)
}

function scheduleSearch() {
  if (timer) clearTimeout(timer)
  timer = setTimeout(() => { void load() }, 300)
}

onMounted(() => { void load() })
onBeforeUnmount(() => { if (timer) clearTimeout(timer) })
</script>

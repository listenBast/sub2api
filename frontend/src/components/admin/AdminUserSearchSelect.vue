<template>
  <div ref="containerRef" class="relative">
    <div class="relative">
      <Icon name="search" size="sm" class="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-gray-400" />
      <input
        v-model="searchQuery"
        type="text"
        autocomplete="off"
        class="user-search-input w-full pl-9 pr-10"
        :placeholder="placeholder"
        :aria-expanded="showDropdown"
        aria-autocomplete="list"
        @input="debounceSearch"
        @focus="openDropdown"
      />
      <button
        v-if="modelValue"
        type="button"
        class="absolute right-2 top-1/2 -translate-y-1/2 rounded p-1 text-gray-400 hover:text-gray-700 dark:hover:text-gray-200"
        :title="t('team.clearSelectedUser')"
        :aria-label="t('team.clearSelectedUser')"
        @click="clearSelection"
      >
        <Icon name="x" size="sm" />
      </button>
    </div>

    <div
      v-if="showDropdown && searchQuery.trim()"
      class="absolute z-[70] mt-1 max-h-60 w-full overflow-auto rounded-md border border-gray-200 bg-white shadow-lg dark:border-dark-600 dark:bg-dark-700"
      role="listbox"
    >
      <div v-if="searchLoading" class="px-4 py-3 text-sm text-gray-500 dark:text-gray-400">
        {{ t('common.loading') }}
      </div>
      <div v-else-if="availableResults.length === 0" class="px-4 py-3 text-sm text-gray-500 dark:text-gray-400">
        {{ t('common.noOptionsFound') }}
      </div>
      <button
        v-for="user in availableResults"
        :key="user.id"
        type="button"
        role="option"
        class="flex w-full items-center justify-between gap-3 px-4 py-2.5 text-left text-sm hover:bg-gray-100 dark:hover:bg-dark-600"
        @click="selectUser(user)"
      >
        <span class="min-w-0">
          <span class="block truncate font-medium text-gray-900 dark:text-white">{{ user.email }}</span>
          <span class="mt-0.5 block truncate text-xs text-gray-500 dark:text-gray-400">{{ user.username || t('team.noUsername') }}</span>
        </span>
        <span class="shrink-0 text-xs text-gray-400">#{{ user.id }}</span>
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type { AdminUser } from '@/types'
import Icon from '@/components/icons/Icon.vue'

const props = withDefaults(defineProps<{
  modelValue: string
  placeholder?: string
  excludeUserIds?: number[]
}>(), {
  placeholder: '',
  excludeUserIds: () => []
})

const emit = defineEmits<{
  'update:modelValue': [value: string]
}>()

const { t } = useI18n()
const containerRef = ref<HTMLElement | null>(null)
const searchQuery = ref(props.modelValue)
const searchResults = ref<AdminUser[]>([])
const searchLoading = ref(false)
const showDropdown = ref(false)
let searchTimer: ReturnType<typeof setTimeout> | null = null
let searchSequence = 0

const availableResults = computed(() => {
  const excluded = new Set(props.excludeUserIds)
  return searchResults.value.filter((user) => user.status === 'active' && user.role === 'user' && !user.deleted_at && !excluded.has(user.id))
})

function clearPendingSearch(): void {
  if (searchTimer) {
    clearTimeout(searchTimer)
    searchTimer = null
  }
  searchSequence += 1
}

function openDropdown(): void {
  showDropdown.value = true
  if (searchQuery.value.trim() && searchResults.value.length === 0) debounceSearch()
}

function debounceSearch(): void {
  clearPendingSearch()
  const query = searchQuery.value.trim()
  showDropdown.value = true
  if (query !== props.modelValue) emit('update:modelValue', '')
  if (!query) {
    searchResults.value = []
    searchLoading.value = false
    return
  }

  const sequence = searchSequence
  searchTimer = setTimeout(async () => {
    searchLoading.value = true
    try {
      const result = await adminAPI.users.list(1, 10, { search: query, status: 'active', role: 'user' })
      if (sequence === searchSequence) searchResults.value = result.items
    } catch {
      if (sequence === searchSequence) searchResults.value = []
    } finally {
      if (sequence === searchSequence) searchLoading.value = false
    }
  }, 300)
}

function selectUser(user: AdminUser): void {
  clearPendingSearch()
  searchQuery.value = user.email
  searchResults.value = []
  searchLoading.value = false
  showDropdown.value = false
  emit('update:modelValue', user.email)
}

function clearSelection(): void {
  clearPendingSearch()
  searchQuery.value = ''
  searchResults.value = []
  searchLoading.value = false
  showDropdown.value = false
  emit('update:modelValue', '')
}

function handleDocumentClick(event: MouseEvent): void {
  const target = event.target as Node | null
  if (target && !containerRef.value?.contains(target)) showDropdown.value = false
}

watch(() => props.modelValue, (value, previousValue) => {
  if (value) searchQuery.value = value
  else if (searchQuery.value === previousValue) searchQuery.value = ''
})

onMounted(() => document.addEventListener('click', handleDocumentClick))
onUnmounted(() => {
  clearPendingSearch()
  document.removeEventListener('click', handleDocumentClick)
})
</script>

<style scoped>
.user-search-input { @apply min-h-11 rounded-md border border-gray-300 bg-white py-2 text-sm text-gray-900 outline-none transition focus:border-primary-500 focus:ring-2 focus:ring-primary-500/20 dark:border-dark-600 dark:bg-dark-800 dark:text-white; }
</style>

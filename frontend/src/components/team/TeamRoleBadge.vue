<template>
  <span class="role-badge" :class="badgeClass" data-testid="team-role-badge">{{ label }}</span>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

const props = defineProps<{
  role: 'owner' | 'admin' | 'member'
}>()

const { t } = useI18n()

const label = computed(() => {
  if (props.role === 'owner') return t('team.roleOwner')
  if (props.role === 'admin') return t('team.roleAdmin')
  return t('team.roleMember')
})

const badgeClass = computed(() => {
  if (props.role === 'owner') return 'role-owner'
  if (props.role === 'admin') return 'role-admin'
  return 'role-member'
})
</script>

<style scoped>
.role-badge { @apply inline-flex shrink-0 items-center rounded px-2 py-0.5 text-xs font-medium; }
.role-owner { @apply bg-primary-50 text-primary-700 dark:bg-primary-950/40 dark:text-primary-300; }
.role-admin { @apply bg-violet-50 text-violet-700 dark:bg-violet-950/40 dark:text-violet-300; }
.role-member { @apply bg-gray-100 text-gray-700 dark:bg-dark-700 dark:text-gray-300; }
</style>

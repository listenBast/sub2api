<template>
  <div class="flex min-w-max items-center justify-end gap-1" role="group" :aria-label="t('team.actions')">
    <button
      type="button"
      class="member-action text-gray-500 hover:bg-primary-50 hover:text-primary-600 dark:text-gray-400 dark:hover:bg-primary-900/20 dark:hover:text-primary-400"
      @click="$emit('edit-remark', member)"
    >
      <Icon name="edit" size="sm" />
      <span class="whitespace-nowrap">{{ t('team.editRemark') }}</span>
    </button>

    <button
      v-if="member.status === 'active' || member.status === 'exit_pending'"
      type="button"
      class="member-action text-gray-500 hover:bg-primary-50 hover:text-primary-600 dark:text-gray-400 dark:hover:bg-primary-900/20 dark:hover:text-primary-400"
      @click="$emit('allocate', member)"
    >
      <Icon name="plus" size="sm" />
      <span class="whitespace-nowrap">{{ t('team.allocate') }}</span>
    </button>

    <template v-if="member.status === 'exit_pending'">
      <button
        type="button"
        class="member-action text-gray-500 hover:bg-emerald-50 hover:text-emerald-600 dark:text-gray-400 dark:hover:bg-emerald-900/20 dark:hover:text-emerald-400"
        @click="$emit('review-exit', member, true)"
      >
        <Icon name="checkCircle" size="sm" />
        <span class="whitespace-nowrap">{{ t('team.approveExit') }}</span>
      </button>
      <button
        type="button"
        class="member-action text-gray-500 hover:bg-amber-50 hover:text-amber-600 dark:text-gray-400 dark:hover:bg-amber-900/20 dark:hover:text-amber-400"
        @click="$emit('review-exit', member, false)"
      >
        <Icon name="xCircle" size="sm" />
        <span class="whitespace-nowrap">{{ t('team.rejectExit') }}</span>
      </button>
    </template>

    <button
      type="button"
      class="member-action text-gray-500 hover:bg-red-50 hover:text-red-600 dark:text-gray-400 dark:hover:bg-red-900/20 dark:hover:text-red-400"
      @click="$emit('remove', member)"
    >
      <Icon :name="member.status === 'invited' ? 'xCircle' : 'trash'" size="sm" />
      <span class="whitespace-nowrap">{{ member.status === 'invited' ? t('team.cancelInvite') : t('team.remove') }}</span>
    </button>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { TeamMember } from '@/api/team'
import Icon from '@/components/icons/Icon.vue'

defineProps<{
  member: TeamMember
}>()

defineEmits<{
  'edit-remark': [member: TeamMember]
  allocate: [member: TeamMember]
  'review-exit': [member: TeamMember, approve: boolean]
  remove: [member: TeamMember]
}>()

const { t } = useI18n()
</script>

<style scoped>
.member-action {
  @apply flex min-h-12 min-w-[64px] flex-col items-center justify-center gap-1 rounded-md px-2 py-1.5 text-center text-xs leading-4 transition-colors;
}

</style>

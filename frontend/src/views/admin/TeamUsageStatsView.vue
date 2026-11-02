<template>
  <AppLayout>
    <div class="space-y-6">
      <AdminTeamPicker v-model="teamId" :initial-team-id="initialTeamId" />
      <p v-if="!teamId" class="py-12 text-center text-sm text-gray-500 dark:text-gray-400">{{ t('team.noTeamSelected') }}</p>
      <TeamMemberSpendPanel v-else :load="loadStats" :reload-key="teamId" @error="showError" />
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { adminTeamsAPI, type TeamSpendRangeParams } from '@/api/team'
import { useAppStore } from '@/stores'
import AppLayout from '@/components/layout/AppLayout.vue'
import AdminTeamPicker from '@/components/team/AdminTeamPicker.vue'
import TeamMemberSpendPanel from '@/components/team/TeamMemberSpendPanel.vue'

const { t } = useI18n()
const route = useRoute()
const appStore = useAppStore()
const teamId = ref<number | null>(null)

const initialTeamId = computed(() => {
  const raw = Number(route.query.team_id)
  return Number.isFinite(raw) && raw > 0 ? raw : null
})

function loadStats(range: TeamSpendRangeParams) {
  return adminTeamsAPI.memberSpendStats(teamId.value as number, range)
}

function showError(error: unknown) {
  appStore.showError((error as { message?: string })?.message || t('common.unknownError'))
}
</script>

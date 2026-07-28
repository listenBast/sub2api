import { computed, ref } from 'vue'
import { defineStore } from 'pinia'
import teamAPI, { type TeamContext } from '@/api/team'

export const useTeamStore = defineStore('team', () => {
  const context = ref<TeamContext | null>(null)
  const loading = ref(false)
  const loaded = ref(false)
  let activeRequest: Promise<TeamContext> | null = null

  const role = computed(() => context.value?.role ?? 'individual')
  const isOwner = computed(() => role.value === 'owner')
  const isMember = computed(() => role.value === 'member')
  const hasInvitation = computed(() => role.value === 'invited')
  const financialRestricted = computed(() => context.value?.financial_restricted === true)

  async function fetchContext(force = false): Promise<TeamContext> {
    if (activeRequest && !force) return activeRequest
    loading.value = true
    const request = teamAPI.getContext()
      .then((data) => {
        context.value = data
        loaded.value = true
        return data
      })
      .finally(() => {
        if (activeRequest === request) {
          activeRequest = null
          loading.value = false
        }
      })
    activeRequest = request
    return request
  }

  function setContext(value: TeamContext) {
    context.value = value
    loaded.value = true
  }

  function clear() {
    activeRequest = null
    context.value = null
    loaded.value = false
    loading.value = false
  }

  return {
    context,
    loading,
    loaded,
    role,
    isOwner,
    isMember,
    hasInvitation,
    financialRestricted,
    fetchContext,
    setContext,
    clear
  }
})

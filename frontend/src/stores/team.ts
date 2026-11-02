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
  /** 团队管理员：拥有除转让主账号外的全部主账号权限。 */
  const isAdmin = computed(() => role.value === 'admin')
  /** 主账号或团队管理员，可使用团队管理功能。 */
  const isManager = computed(() => role.value === 'owner' || role.value === 'admin')
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
    isAdmin,
    isManager,
    isMember,
    hasInvitation,
    financialRestricted,
    fetchContext,
    setContext,
    clear
  }
})

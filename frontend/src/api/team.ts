import { apiClient } from './client'
import type { PaginatedResponse } from '@/types'

export type TeamRole = 'individual' | 'owner' | 'member' | 'invited'
export type TeamStatus = 'active' | 'suspended'
export type TeamMembershipStatus = 'invited' | 'active' | 'exit_pending'

export interface TeamOwner {
  user_id: number
  email: string
  username: string
  balance: number
}

export interface TeamMember {
  membership_id: number
  user_id: number
  email: string
  username: string
  remark: string
  status: TeamMembershipStatus
  balance: number
  frozen_balance: number
  concurrency: number
  rpm_limit: number
  joined_at?: string | null
  exit_requested_at?: string | null
  created_at: string
}

export interface TeamSummary {
  id: number
  name: string
  status: TeamStatus
  owner: TeamOwner
  members: TeamMember[]
  active_member_count: number
  pending_invite_count: number
  exit_pending_count: number
  total_balance: number
  created_at: string
}

export interface TeamAdminOverview {
  total_teams: number
  active_teams: number
  suspended_teams: number
  member_count: number
  pending_invites: number
  exit_pending: number
  total_balance: number
}

export interface TeamContext {
  role: TeamRole
  membership_status?: TeamMembershipStatus
  financial_restricted: boolean
  team?: TeamSummary
  current_membership?: TeamMember
}

export interface TeamTransaction {
  id: number
  team_id: number
  operator_id: number
  member_id?: number | null
  member_email?: string
  member_username?: string
  member_remark?: string
  action: string
  amount: number
  owner_balance_before: number
  owner_balance_after: number
  member_balance_before?: number | null
  member_balance_after?: number | null
  note: string
  created_at: string
}

export interface TeamUsageItem {
  id: number
  user_id: number
  member_email: string
  request_id: string
  model: string
  input_tokens: number
  output_tokens: number
  cache_tokens: number
  total_tokens: number
  total_cost: number
  actual_cost: number
  duration_ms?: number | null
  created_at: string
}

export interface TeamTrendPoint {
  date: string
  requests: number
  tokens: number
  actual_cost: number
}

export interface TeamMemberUsageStat {
  user_id: number
  email: string
  username: string
  requests: number
  tokens: number
  actual_cost: number
}

export interface TeamDashboard {
  total_requests: number
  total_tokens: number
  total_cost: number
  actual_cost: number
  member_count: number
  team_balance: number
  trend: TeamTrendPoint[]
  members: TeamMemberUsageStat[]
  start: string
  end: string
}

export interface TeamUsageParams {
  page?: number
  page_size?: number
  member_id?: number
  model?: string
  start_date?: string
  end_date?: string
}

export function normalizeTeamSummary(team: TeamSummary): TeamSummary {
  return { ...team, members: team.members ?? [] }
}

export function normalizeTeamContext(context: TeamContext): TeamContext {
  if (!context.team) return context
  return { ...context, team: normalizeTeamSummary(context.team) }
}

export async function getContext(): Promise<TeamContext> {
  const { data } = await apiClient.get<TeamContext>('/team')
  return normalizeTeamContext(data)
}

export async function upgrade(name: string): Promise<TeamContext> {
  const { data } = await apiClient.post<TeamContext>('/team/upgrade', { name })
  return normalizeTeamContext(data)
}

export async function rename(name: string): Promise<TeamContext> {
  const { data } = await apiClient.put<TeamContext>('/team', { name })
  return normalizeTeamContext(data)
}

export async function invite(email: string): Promise<TeamMember> {
  const { data } = await apiClient.post<TeamMember>('/team/invitations', { email })
  return data
}

export async function respondInvitation(accept: boolean): Promise<TeamContext> {
  const { data } = await apiClient.post<TeamContext>('/team/invitations/respond', { accept })
  return normalizeTeamContext(data)
}

export async function allocateBalance(memberId: number, amount: number, note = ''): Promise<TeamMember> {
  const { data } = await apiClient.post<TeamMember>(`/team/members/${memberId}/balance`, { amount, note })
  return data
}

export async function requestExit(): Promise<TeamContext> {
  const { data } = await apiClient.post<TeamContext>('/team/exit')
  return normalizeTeamContext(data)
}

export async function reviewExit(memberId: number, approve: boolean): Promise<void> {
  await apiClient.post(`/team/members/${memberId}/exit-review`, { approve })
}

export async function removeMember(memberId: number): Promise<void> {
  await apiClient.delete(`/team/members/${memberId}`)
}

export async function updateMemberRemark(memberId: number, remark: string): Promise<TeamMember> {
  const { data } = await apiClient.patch<TeamMember>(`/team/members/${memberId}/remark`, { remark })
  return data
}

export async function updateMemberLimits(memberId: number, limits: { concurrency?: number; rpm_limit?: number }): Promise<TeamMember> {
  const { data } = await apiClient.patch<TeamMember>(`/team/members/${memberId}/limits`, limits)
  return data
}

export async function dissolve(): Promise<void> {
  await apiClient.delete('/team')
}

export async function getDashboard(params: { start_date?: string; end_date?: string } = {}): Promise<TeamDashboard> {
  const { data } = await apiClient.get<TeamDashboard>('/team/dashboard', { params })
  return data
}

export async function listUsage(params: TeamUsageParams): Promise<PaginatedResponse<TeamUsageItem>> {
  const { data } = await apiClient.get<PaginatedResponse<TeamUsageItem>>('/team/usage', { params })
  return data
}

export async function listTransactions(page = 1, pageSize = 20): Promise<PaginatedResponse<TeamTransaction>> {
  const { data } = await apiClient.get<PaginatedResponse<TeamTransaction>>('/team/transactions', {
    params: { page, page_size: pageSize }
  })
  return data
}

export const adminTeamsAPI = {
  async list(params: { page?: number; page_size?: number; status?: string; search?: string }): Promise<PaginatedResponse<TeamSummary>> {
    const { data } = await apiClient.get<PaginatedResponse<TeamSummary>>('/admin/teams', { params })
    return { ...data, items: data.items.map(normalizeTeamSummary) }
  },
  async get(teamId: number): Promise<TeamSummary> {
    const { data } = await apiClient.get<TeamSummary>(`/admin/teams/${teamId}`)
    return normalizeTeamSummary(data)
  },
  async overview(): Promise<TeamAdminOverview> {
    const { data } = await apiClient.get<TeamAdminOverview>('/admin/teams/overview')
    return data
  },
  async create(payload: { name: string; owner_email: string }): Promise<TeamSummary> {
    const { data } = await apiClient.post<TeamSummary>('/admin/teams', payload)
    return normalizeTeamSummary(data)
  },
  async setStatus(teamId: number, status: TeamStatus, reason: string): Promise<TeamSummary> {
    const { data } = await apiClient.put<TeamSummary>(`/admin/teams/${teamId}/status`, { status, reason })
    return normalizeTeamSummary(data)
  },
  async deleteTeam(teamId: number): Promise<void> {
    await apiClient.delete(`/admin/teams/${teamId}`)
  },
  async removeMember(teamId: number, memberId: number): Promise<void> {
    await apiClient.delete(`/admin/teams/${teamId}/members/${memberId}`)
  },
  async addMember(teamId: number, payload: { email: string; remark?: string }): Promise<TeamMember> {
    const { data } = await apiClient.post<TeamMember>(`/admin/teams/${teamId}/members`, payload)
    return data
  },
  async updateMemberRemark(teamId: number, memberId: number, remark: string): Promise<TeamMember> {
    const { data } = await apiClient.patch<TeamMember>(`/admin/teams/${teamId}/members/${memberId}/remark`, { remark })
    return data
  },
  async dashboard(teamId: number, params: { start_date?: string; end_date?: string } = {}): Promise<TeamDashboard> {
    const { data } = await apiClient.get<TeamDashboard>(`/admin/teams/${teamId}/dashboard`, { params })
    return data
  },
  async usage(teamId: number, params: TeamUsageParams): Promise<PaginatedResponse<TeamUsageItem>> {
    const { data } = await apiClient.get<PaginatedResponse<TeamUsageItem>>(`/admin/teams/${teamId}/usage`, { params })
    return data
  },
  async transactions(teamId: number, page = 1, pageSize = 20): Promise<PaginatedResponse<TeamTransaction>> {
    const { data } = await apiClient.get<PaginatedResponse<TeamTransaction>>(`/admin/teams/${teamId}/transactions`, {
      params: { page, page_size: pageSize }
    })
    return data
  }
}

export default {
  getContext,
  upgrade,
  rename,
  invite,
  respondInvitation,
  allocateBalance,
  requestExit,
  reviewExit,
  removeMember,
  updateMemberRemark,
  updateMemberLimits,
  dissolve,
  getDashboard,
  listUsage,
  listTransactions
}

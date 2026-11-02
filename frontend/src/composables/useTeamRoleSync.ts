import { onBeforeUnmount, onMounted } from 'vue'
import { useTeamStore } from '@/stores'

export interface TeamRoleSyncOptions {
  /** 轮询间隔（毫秒），页面可见时才会轮询。默认 30 秒。 */
  intervalMs?: number
  /** 两次刷新之间的最小间隔（毫秒），避免 focus / visibility 事件连发。默认 3 秒。 */
  minGapMs?: number
  /** 返回 false 时不监听（例如平台管理员没有团队身份）。默认始终监听。 */
  enabled?: () => boolean
}

/**
 * 让团队角色变更（例如被主账号设为 / 取消管理员）实时反映到当前页面：
 * 角色由服务端每次请求从数据库判定，这里负责让前端尽快拿到最新角色——
 * 窗口重新获得焦点、标签页重新可见、以及页面可见期间的定时轮询都会刷新团队上下文。
 * 页面切换时侧边栏会重新挂载并立即刷新一次，因此路由跳转同样会拿到最新角色。
 */
export function useTeamRoleSync(options: TeamRoleSyncOptions = {}) {
  const intervalMs = options.intervalMs ?? 30_000
  const minGapMs = options.minGapMs ?? 3_000
  const teamStore = useTeamStore()
  let timer: ReturnType<typeof setInterval> | null = null
  let lastRefreshAt = 0

  function refresh(force = false) {
    const now = Date.now()
    if (!force && now - lastRefreshAt < minGapMs) return
    lastRefreshAt = now
    // 静默失败：网络抖动不应打断用户，下一次触发会再试。
    void teamStore.fetchContext(true).catch(() => undefined)
  }

  function onFocus() {
    refresh()
  }

  function onVisibilityChange() {
    if (typeof document !== 'undefined' && document.visibilityState === 'visible') refresh()
  }

  function start() {
    if (typeof window === 'undefined') return
    window.addEventListener('focus', onFocus)
    document.addEventListener('visibilitychange', onVisibilityChange)
    timer = setInterval(() => {
      if (document.visibilityState === 'visible') refresh()
    }, intervalMs)
  }

  function stop() {
    if (typeof window === 'undefined') return
    window.removeEventListener('focus', onFocus)
    document.removeEventListener('visibilitychange', onVisibilityChange)
    if (timer) clearInterval(timer)
    timer = null
  }

  onMounted(() => {
    if (options.enabled?.() ?? true) start()
  })
  onBeforeUnmount(stop)

  return { refresh, start, stop }
}

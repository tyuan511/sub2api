import { defineStore } from 'pinia'
import { ref } from 'vue'
import { getSupportUnreadCount } from '@/api/support'
import { getAdminSupportUnreadCount } from '@/api/admin/support'
import { getAPIBaseURL } from '@/api/url'

export const useSupportStore = defineStore('support', () => {
  const unreadCount = ref(0)
  let socket: WebSocket | null = null
  let reconnectTimer: ReturnType<typeof setTimeout> | null = null
  let unreadRefreshTimer: ReturnType<typeof setTimeout> | null = null
  let stopped = true
  let adminMode = false

  async function refreshUnread(isAdmin?: boolean) {
    if (typeof isAdmin === 'boolean') adminMode = isAdmin
    try {
      unreadCount.value = adminMode ? await getAdminSupportUnreadCount() : await getSupportUnreadCount()
    } catch {
      // Navigation remains usable when support is temporarily unavailable.
    }
  }

  function socketURL() {
    const path = `${getAPIBaseURL().replace(/\/+$/, '')}${adminMode ? '/admin' : ''}/support/ws`
    const target = new URL(path, window.location.origin)
    target.protocol = target.protocol === 'https:' ? 'wss:' : 'ws:'
    return target.toString()
  }

  function connect(isAdmin: boolean) {
    stop()
    stopped = false
    adminMode = isAdmin
    void refreshUnread()
    const token = localStorage.getItem('auth_token')
    if (!token) return
    socket = new WebSocket(socketURL(), ['sub2api-support', `jwt.${token}`])
    socket.onmessage = () => {
      window.dispatchEvent(new CustomEvent('support-realtime'))
      // Let the active conversation mark itself read before refreshing the
      // badge; otherwise a slower pre-read request can restore a stale count.
      if (unreadRefreshTimer) clearTimeout(unreadRefreshTimer)
      unreadRefreshTimer = setTimeout(() => {
        unreadRefreshTimer = null
        void refreshUnread()
      }, 250)
    }
    socket.onclose = () => {
      socket = null
      if (!stopped) reconnectTimer = setTimeout(() => connect(adminMode), 3000)
    }
  }

  function stop() {
    stopped = true
    if (reconnectTimer) clearTimeout(reconnectTimer)
    if (unreadRefreshTimer) clearTimeout(unreadRefreshTimer)
    reconnectTimer = null
    unreadRefreshTimer = null
    if (socket) socket.close()
    socket = null
  }

  return { unreadCount, refreshUnread, connect, stop }
})

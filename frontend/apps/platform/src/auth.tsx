import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import type { User } from '@mscs/shared'
import { Navigate, Outlet, useLocation } from 'react-router'
import { api, ApiError } from './api'

const ME = ['me'] as const

/** Who is logged in to the console; null when nobody is (401/403). */
export function useMe() {
  return useQuery({
    queryKey: ME,
    queryFn: async () => {
      try {
        return (await api<{ user: User }>('/me')).user
      }
      catch (e) {
        if (e instanceof ApiError && (e.status === 401 || e.status === 403)) return null
        throw e
      }
    },
    staleTime: 5 * 60_000,
  })
}

export function useLogin() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: { email: string, password: string }) => api<{ user: User }>('/login', { method: 'POST', body }),
    onSuccess: ({ user }) => qc.setQueryData(ME, user),
  })
}

export function useLogout() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: () => api<void>('/logout', { method: 'POST' }),
    // Forget every cached answer: the next person on this browser must not see them.
    onSettled: () => {
      qc.clear()
      qc.setQueryData(ME, null)
    },
  })
}

/** Route guard: renders the child routes for a logged-in admin, otherwise sends to /login. */
export function RequireAdmin() {
  const me = useMe()
  const location = useLocation()
  if (me.isPending) {
    return <p className="p-8 text-center text-sm text-ink-muted">กำลังโหลด…</p>
  }
  if (me.isError) {
    return <p className="p-8 text-center text-sm text-danger">เชื่อมต่อเซิร์ฟเวอร์ไม่ได้ กรุณาลองใหม่อีกครั้ง</p>
  }
  if (!me.data) {
    return <Navigate to="/login" replace state={{ from: location.pathname }} />
  }
  return <Outlet />
}

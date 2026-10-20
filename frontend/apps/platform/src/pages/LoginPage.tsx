import { useState, type FormEvent } from 'react'
import { Navigate, useLocation, useNavigate } from 'react-router'
import { errorMessage } from '../api'
import { useLogin, useMe } from '../auth'

export function LoginPage() {
  const me = useMe()
  const login = useLogin()
  const navigate = useNavigate()
  const from = (useLocation().state as { from?: string } | null)?.from ?? '/'
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')

  if (me.data) return <Navigate to={from} replace />

  function onSubmit(e: FormEvent) {
    e.preventDefault()
    login.mutate({ email: email.trim(), password }, { onSuccess: () => navigate(from, { replace: true }) })
  }

  return (
    <div className="flex min-h-dvh items-center justify-center px-4">
      <form onSubmit={onSubmit} className="w-full max-w-sm space-y-4 rounded-xl border border-line bg-surface p-6 shadow-sm">
        <div>
          <h1 className="text-xl font-semibold">ผู้ดูแลแพลตฟอร์ม MSCS</h1>
          <p className="text-sm text-ink-muted">สำหรับทีมงานที่ดูแลระบบเท่านั้น ไม่ใช่สำหรับร้านค้า</p>
        </div>
        <label className="block space-y-1 text-sm">
          <span>อีเมล</span>
          <input
            type="email"
            required
            autoComplete="username"
            value={email}
            onChange={e => setEmail(e.target.value)}
            className="w-full rounded-md border border-line bg-surface px-3 py-2 outline-accent"
          />
        </label>
        <label className="block space-y-1 text-sm">
          <span>รหัสผ่าน</span>
          <input
            type="password"
            required
            autoComplete="current-password"
            value={password}
            onChange={e => setPassword(e.target.value)}
            className="w-full rounded-md border border-line bg-surface px-3 py-2 outline-accent"
          />
        </label>
        {login.isError && (
          <p role="alert" className="rounded-md bg-danger-soft px-3 py-2 text-sm text-danger">
            {errorMessage(login.error)}
          </p>
        )}
        <button
          type="submit"
          disabled={login.isPending}
          className="w-full rounded-md bg-accent px-3 py-2 font-medium text-accent-ink disabled:opacity-60"
        >
          {login.isPending ? 'กำลังเข้าสู่ระบบ…' : 'เข้าสู่ระบบ'}
        </button>
      </form>
    </div>
  )
}

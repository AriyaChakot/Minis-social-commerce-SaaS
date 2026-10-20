import { NavLink, Outlet, useNavigate } from 'react-router'
import { useLogout, useMe } from '../auth'

const NAV = [
  { to: '/', label: 'ภาพรวม', end: true },
  { to: '/tenants', label: 'ร้านค้า', end: false },
]

export function Layout() {
  const me = useMe()
  const logout = useLogout()
  const navigate = useNavigate()

  return (
    <div className="min-h-dvh">
      <header className="border-b border-line bg-surface">
        <div className="mx-auto flex max-w-6xl flex-wrap items-center gap-x-6 gap-y-2 px-4 py-3">
          <p className="font-semibold">
            MSCS <span className="rounded bg-accent px-1.5 py-0.5 text-xs text-accent-ink">Platform</span>
          </p>
          <nav className="flex gap-1">
            {NAV.map(n => (
              <NavLink
                key={n.to}
                to={n.to}
                end={n.end}
                className={({ isActive }) =>
                  `rounded-md px-3 py-1.5 text-sm ${isActive ? 'bg-ok-soft font-medium text-ink' : 'text-ink-muted hover:text-ink'}`}
              >
                {n.label}
              </NavLink>
            ))}
          </nav>
          <div className="ml-auto flex min-w-0 items-center gap-3 text-sm">
            <span className="truncate text-ink-muted">{me.data?.email}</span>
            <button
              type="button"
              className="shrink-0 rounded-md border border-line px-3 py-1.5 hover:bg-page disabled:opacity-50"
              disabled={logout.isPending}
              onClick={() => logout.mutate(undefined, { onSettled: () => navigate('/login', { replace: true }) })}
            >
              ออกจากระบบ
            </button>
          </div>
        </div>
      </header>
      <main className="mx-auto max-w-6xl px-4 py-6">
        <Outlet />
      </main>
    </div>
  )
}

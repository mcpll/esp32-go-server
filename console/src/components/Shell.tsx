import { Suspense } from 'react'
import { NavLink, Outlet, useLocation } from 'react-router-dom'
import { PageErrorBoundary } from '@/components/PageErrorBoundary'
import { Button } from '@/components/ui/button'
import { useSession } from '@/hooks/useSession'
import { AGENTS_PATH, DEVICES_PATH } from '@/lib/gate'
import { cn } from '@/lib/utils'

const LINKS = [
  { to: AGENTS_PATH, label: 'Agents' },
  { to: DEVICES_PATH, label: 'Devices' },
] as const

export function Shell() {
  const { pathname } = useLocation()
  const { state, actions } = useSession()

  return (
    <div className="mx-auto flex min-h-svh max-w-3xl flex-col gap-6 p-6">
      <header className="flex flex-wrap items-center justify-between gap-4">
        <nav className="flex gap-4">
          {LINKS.map((link) => (
            <NavLink
              key={link.to}
              to={link.to}
              className={({ isActive }) =>
                cn('text-sm', isActive ? 'font-semibold' : 'text-muted-foreground')
              }
            >
              {link.label}
            </NavLink>
          ))}
        </nav>
        <div className="flex items-center gap-3">
          <span className="text-sm text-muted-foreground">{state.email}</span>
          <Button type="button" variant="outline" onClick={actions.logout}>
            Log out
          </Button>
        </div>
      </header>
      <PageErrorBoundary key={pathname}>
        <Suspense fallback={<p>Loading…</p>}>
          <Outlet />
        </Suspense>
      </PageErrorBoundary>
    </div>
  )
}

import { Suspense, type ComponentType } from 'react'
import { NavLink, Outlet, useLocation } from 'react-router-dom'
import { AudioLines, Bot, LogOut, Radio, Settings } from 'lucide-react'
import { PageErrorBoundary } from '@/components/PageErrorBoundary'
import { useSession } from '@/hooks/useSession'
import { AGENTS_PATH, DEVICES_PATH, SETTINGS_PATH } from '@/lib/gate'
import { cn } from '@/lib/utils'

const LINKS: { to: string; label: string; icon: ComponentType<{ className?: string; strokeWidth?: number }> }[] = [
  { to: AGENTS_PATH, label: 'Agents', icon: Bot },
  { to: DEVICES_PATH, label: 'Devices', icon: Radio },
  { to: SETTINGS_PATH, label: 'Settings', icon: Settings },
]

const railItem =
  'flex size-10 items-center justify-center rounded-xl transition-colors focus-visible:outline-none focus-visible:ring-3 focus-visible:ring-ring/50'

export function Shell() {
  const { pathname } = useLocation()
  const { state, actions } = useSession()
  const initial = state.email.trim().slice(0, 1).toUpperCase()

  return (
    <div className="flex h-svh overflow-hidden bg-background">
      <nav className="flex w-14 shrink-0 flex-col items-center border-r border-border bg-card py-3">
        <div className="mb-4 flex size-9 items-center justify-center rounded-full bg-accent text-primary">
          <AudioLines className="size-4" strokeWidth={2.2} />
        </div>
        <div className="flex flex-1 flex-col items-center gap-1">
          {LINKS.map((link) => {
            const Icon = link.icon
            return (
              <NavLink
                key={link.to}
                to={link.to}
                title={link.label}
                aria-label={link.label}
                className={({ isActive }) =>
                  cn(railItem, isActive ? 'bg-accent text-primary' : 'text-muted-foreground hover:bg-black/4 hover:text-foreground')
                }
              >
                <Icon className="size-[18px]" strokeWidth={1.8} />
              </NavLink>
            )
          })}
        </div>
        {initial !== '' ? (
          <span
            title={state.email}
            className="mb-1 flex size-8 items-center justify-center rounded-full bg-muted text-xs font-medium text-muted-foreground"
          >
            {initial}
          </span>
        ) : null}
        <button
          type="button"
          title="Log out"
          aria-label="Log out"
          onClick={actions.logout}
          className={cn(railItem, 'text-muted-foreground hover:bg-black/4 hover:text-foreground')}
        >
          <LogOut className="size-[18px]" strokeWidth={1.8} />
        </button>
      </nav>
      <main className="flex min-h-0 min-w-0 flex-1 flex-col bg-card">
        <PageErrorBoundary key={pathname}>
          <Suspense
            fallback={
              <p className="flex flex-1 items-center justify-center text-sm text-muted-foreground">Loading…</p>
            }
          >
            <Outlet />
          </Suspense>
        </PageErrorBoundary>
      </main>
    </div>
  )
}

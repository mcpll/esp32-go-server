import { useState } from 'react'
import { SettingsBlock } from '@/components/SettingsBlock'
import { useSettings } from '@/hooks/useSettings'
import { blockFor, SETTINGS_BLOCKS } from '@/lib/settingsForm'
import { cn } from '@/lib/utils'

export function SettingsPage() {
  const { data } = useSettings()
  const known = new Set(SETTINGS_BLOCKS.map((block) => block.key))
  const byKey = new Map(data.map((setting) => [setting.key, setting]))
  const ordered = [
    ...SETTINGS_BLOCKS.flatMap((block) => {
      const setting = byKey.get(block.key)
      return setting === undefined ? [] : [setting]
    }),
    ...data.filter((setting) => !known.has(setting.key)),
  ]
  const [picked, setPicked] = useState<string | null>(ordered[0]?.key ?? null)
  const [actionsHost, setActionsHost] = useState<HTMLDivElement | null>(null)
  const active = ordered.find((setting) => setting.key === picked) ?? ordered[0] ?? null

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <header className="flex h-14 shrink-0 items-center gap-2 border-b border-border px-3">
        <h1 className="min-w-0 flex-1 truncate text-sm font-semibold">Settings</h1>
        <div ref={setActionsHost} className="flex shrink-0 items-center gap-2" />
      </header>
      {ordered.length === 0 ? (
        <p className="px-6 py-6 text-sm text-muted-foreground">No settings yet.</p>
      ) : (
        <div className="flex min-h-0 flex-1 flex-col md:flex-row">
          <nav
            aria-label="Settings"
            className="flex shrink-0 gap-1 overflow-x-auto border-b border-border px-3 py-2 md:w-52 md:flex-col md:overflow-y-auto md:border-r md:border-b-0 md:py-3"
          >
            {ordered.map((setting) => {
              const selected = setting.key === active?.key
              return (
                <button
                  key={setting.id}
                  type="button"
                  aria-current={selected ? 'true' : undefined}
                  className={cn(
                    'shrink-0 rounded-full px-3 py-1.5 text-left text-sm focus-visible:outline-none focus-visible:ring-3 focus-visible:ring-ring/50 md:rounded-xl',
                    selected ? 'bg-accent font-medium text-primary' : 'text-muted-foreground hover:bg-muted hover:text-foreground',
                  )}
                  onClick={() => setPicked(setting.key)}
                >
                  {blockFor(setting.key)?.title ?? setting.key}
                </button>
              )
            })}
          </nav>
          {ordered.map((setting) => (
            <div
              key={setting.id}
              className={cn('min-h-0 min-w-0 flex-1 flex-col', setting.key === active?.key ? 'flex' : 'hidden')}
            >
              <SettingsBlock setting={setting} actionsHost={setting.key === active?.key ? actionsHost : null} />
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

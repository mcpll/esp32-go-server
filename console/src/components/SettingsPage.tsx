import { SettingsBlock } from '@/components/SettingsBlock'
import { useSettings } from '@/hooks/useSettings'
import { SETTINGS_BLOCKS } from '@/lib/settingsForm'

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

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <header className="flex h-14 shrink-0 items-center border-b border-border px-4">
        <h1 className="text-sm font-semibold">Settings</h1>
      </header>
      <div className="flex-1 overflow-y-auto px-6 py-6">
        {ordered.length === 0 ? (
          <p className="text-sm text-muted-foreground">No settings yet.</p>
        ) : (
          <div className="grid max-w-xl gap-5">
            {ordered.map((setting) => (
              <SettingsBlock key={setting.id} setting={setting} />
            ))}
          </div>
        )}
      </div>
    </div>
  )
}

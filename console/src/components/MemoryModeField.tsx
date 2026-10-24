import { Label } from '@/components/ui/label'
import { memoryChoices, type MemoryMode } from '@/lib/memoryMode'
import { cn } from '@/lib/utils'

export function MemoryModeField({
  mode,
  redisEnabled,
  onChange,
}: {
  mode: MemoryMode
  redisEnabled: boolean
  onChange: (mode: MemoryMode) => void
}) {
  const offered = memoryChoices(redisEnabled)
  return (
    <div className="grid gap-2">
      <Label id="memory-mode-label">Memory</Label>
      <div
        role="radiogroup"
        aria-labelledby="memory-mode-label"
        className="flex gap-1 rounded-full bg-muted p-1"
      >
        {offered.choices.map((choice) => (
          <button
            key={choice.mode}
            type="button"
            role="radio"
            aria-checked={mode === choice.mode}
            disabled={choice.disabled}
            className={cn(
              'flex-1 rounded-full px-3 py-1.5 text-sm font-medium focus-visible:outline-none focus-visible:ring-3 focus-visible:ring-ring/50 disabled:cursor-not-allowed disabled:opacity-50',
              mode === choice.mode
                ? 'bg-card text-foreground shadow-soft'
                : 'text-muted-foreground hover:text-foreground',
            )}
            onClick={() => onChange(choice.mode)}
          >
            {choice.label}
          </button>
        ))}
      </div>
      {offered.note !== null ? (
        <p className="text-sm text-muted-foreground">{offered.note}</p>
      ) : null}
    </div>
  )
}

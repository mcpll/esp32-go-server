import { useState, type ReactNode } from 'react'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Label } from '@/components/ui/label'
import { errorMessage } from '@/lib/errors'
import { knownProviders, withProvider, type ProviderDraft, type Stage } from '@/lib/providerForm'

const OTHER = '__other__'

export function ProviderSelect({
  stage,
  draft,
  onChange,
}: {
  stage: Stage
  draft: ProviderDraft
  onChange: (draft: ProviderDraft) => void
}) {
  const [error, setError] = useState<string | null>(null)
  const items: Record<string, ReactNode> = {}
  for (const provider of knownProviders(stage)) {
    items[provider] = provider
  }
  items[OTHER] = 'Other'
  const selected = draft.kind === 'known' ? draft.provider : OTHER

  function onValueChange(value: string | null) {
    if (value === null) return
    try {
      onChange(withProvider(stage, draft, value === OTHER ? '' : value))
      setError(null)
    } catch (caught) {
      setError(errorMessage(caught))
    }
  }

  return (
    <div className="grid gap-2">
      <Label htmlFor={`${stage}-provider`}>Provider</Label>
      <Select items={items} value={selected} onValueChange={onValueChange}>
        <SelectTrigger id={`${stage}-provider`} className="w-full">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {Object.entries(items).map(([value, label]) => (
            <SelectItem key={value} value={value}>
              {label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      {error !== null ? <p role="alert">{error}</p> : null}
    </div>
  )
}

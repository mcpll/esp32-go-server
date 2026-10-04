import { areaClass, controlClass } from '@/components/classes'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import type { RawDraft, Stage } from '@/lib/providerForm'

export function RawProviderConfig({
  stage,
  draft,
  onChange,
}: {
  stage: Stage
  draft: RawDraft
  onChange: (draft: RawDraft) => void
}) {
  const providerId = `${stage}-raw-provider`
  const jsonId = `${stage}-raw-json`
  return (
    <div className="grid gap-4">
      <div className="grid gap-2">
        <Label htmlFor={providerId}>Provider name</Label>
        <Input
          id={providerId}
          className={controlClass}
          value={draft.provider}
          onChange={(event) => onChange({ ...draft, provider: event.target.value })}
        />
      </div>
      <div className="grid gap-2">
        <Label htmlFor={jsonId}>Config JSON</Label>
        <Textarea
          id={jsonId}
          className={`${areaClass} font-mono`}
          rows={8}
          spellCheck={false}
          value={draft.restText}
          onChange={(event) => onChange({ ...draft, restText: event.target.value })}
        />
      </div>
    </div>
  )
}

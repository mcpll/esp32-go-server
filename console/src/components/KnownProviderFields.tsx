import { areaClass } from '@/components/classes'
import { FieldControl } from '@/components/FieldControl'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { fieldsFor, type KnownDraft, type Stage } from '@/lib/providerForm'

export function KnownProviderFields({
  stage,
  draft,
  onChange,
}: {
  stage: Stage
  draft: KnownDraft
  onChange: (draft: KnownDraft) => void
}) {
  const fields = fieldsFor(stage, draft.provider) ?? []
  return (
    <div className="grid gap-4">
      {fields.map((field) => (
        <FieldControl
          key={field.path}
          id={`${stage}-${field.path.replace('.', '-')}`}
          field={field}
          value={draft.values[field.path]}
          onChange={(value) =>
            onChange({ ...draft, values: { ...draft.values, [field.path]: value } })
          }
        />
      ))}
      <div className="grid gap-2">
        <Label htmlFor={`${stage}-json`}>Other JSON</Label>
        <Textarea
          id={`${stage}-json`}
          className={`${areaClass} font-mono`}
          rows={6}
          spellCheck={false}
          value={draft.restText}
          onChange={(event) => onChange({ ...draft, restText: event.target.value })}
        />
      </div>
    </div>
  )
}

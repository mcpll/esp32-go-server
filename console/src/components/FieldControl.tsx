import { controlClass } from '@/components/classes'
import type { Field } from '@/lib/providerForm'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'

export function FieldControl({
  id,
  field,
  value,
  onChange,
  hint,
}: {
  id: string
  field: Field
  value: string | boolean | undefined
  onChange: (value: string | boolean) => void
  hint?: string
}) {
  const label = (
    <Label htmlFor={id}>
      {field.label}
      {hint ? <span className="ml-2 font-normal text-muted-foreground">{hint}</span> : null}
    </Label>
  )
  if (field.kind === 'bool') {
    return (
      <div className="flex items-center justify-between gap-4">
        {label}
        <Switch id={id} checked={value === true} onCheckedChange={onChange} />
      </div>
    )
  }

  const text = typeof value === 'string' ? value : ''
  return (
    <div className="grid gap-2">
      {label}
      <Input
        id={id}
        className={controlClass}
        inputMode={field.kind === 'number' ? 'numeric' : 'text'}
        value={text}
        onChange={(event) => onChange(event.target.value)}
      />
    </div>
  )
}

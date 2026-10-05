import { controlClass } from '@/components/classes'
import { FieldInfo } from '@/components/FieldInfo'
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
  info,
  masked = false,
}: {
  id: string
  field: Field
  value: string | boolean | undefined
  onChange: (value: string | boolean) => void
  hint?: string
  info?: string
  masked?: boolean
}) {
  const label = (
    <span className="flex items-center gap-1.5">
      <Label htmlFor={id}>
        {field.label}
        {hint ? <span className="ml-2 font-normal text-muted-foreground">{hint}</span> : null}
      </Label>
      {info !== undefined && info !== '' ? <FieldInfo text={info} /> : null}
    </span>
  )
  if (field.kind === 'bool') {
    return (
      <div className="grid gap-2">
        {label}
        <div className="flex h-11 items-center">
          <Switch id={id} checked={value === true} onCheckedChange={onChange} />
        </div>
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
        type={masked ? 'password' : 'text'}
        autoComplete={masked ? 'off' : undefined}
        inputMode={field.kind === 'number' ? 'numeric' : 'text'}
        value={text}
        onChange={(event) => onChange(event.target.value)}
      />
    </div>
  )
}

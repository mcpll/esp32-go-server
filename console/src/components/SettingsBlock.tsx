import { useState, type FormEvent } from 'react'
import { actionClass, areaClass, panelClass } from '@/components/classes'
import { FieldControl } from '@/components/FieldControl'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { useSaveSetting } from '@/hooks/useSettings'
import { errorMessage } from '@/lib/errors'
import {
  blockFor,
  fieldValues,
  isRestartRequired,
  rawSettingsValue,
  settingsValue,
  type FieldValues,
  type Setting,
} from '@/lib/settingsForm'

export function SettingsBlock({ setting }: { setting: Setting }) {
  const block = blockFor(setting.key)
  const save = useSaveSetting()
  const [values, setValues] = useState<FieldValues>(() =>
    block === null ? { raw: JSON.stringify(setting.value, null, 2) } : fieldValues(setting.value, block),
  )
  const [invalid, setInvalid] = useState<string | null>(null)
  const message = invalid ?? (save.error === null ? null : errorMessage(save.error))

  function onSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setInvalid(null)
    try {
      const value =
        block === null ? rawSettingsValue(stringValue(values.raw)) : settingsValue(block, setting.value, values)
      save.mutate({ id: setting.id, value })
    } catch (error) {
      setInvalid(errorMessage(error))
    }
  }

  return (
    <Card className={panelClass}>
      <CardHeader>
        <CardTitle>{block?.title ?? setting.key}</CardTitle>
      </CardHeader>
      <CardContent>
        <form className="grid gap-4" onSubmit={onSubmit}>
          {block === null ? (
            <div className="grid gap-2">
              <Label htmlFor={`${setting.id}-raw`}>Value</Label>
              <Textarea
                id={`${setting.id}-raw`}
                className={areaClass}
                value={stringValue(values.raw)}
                onChange={(event) => setValues({ raw: event.target.value })}
              />
            </div>
          ) : (
            block.fields.map((field) => {
              const hint = isRestartRequired(block.key, field.path) ? 'Restart required' : undefined
              const { kind } = field
              if (kind === 'json') {
                return (
                  <div key={field.path} className="grid gap-2">
                    <Label htmlFor={`${setting.id}-${field.path}`}>
                      {field.label}
                      {hint ? <span className="ml-2 font-normal text-muted-foreground">{hint}</span> : null}
                    </Label>
                    <Textarea
                      id={`${setting.id}-${field.path}`}
                      className={areaClass}
                      spellCheck={false}
                      value={stringValue(values[field.path])}
                      onChange={(event) =>
                        setValues((current) => ({ ...current, [field.path]: event.target.value }))
                      }
                    />
                  </div>
                )
              }
              return (
                <FieldControl
                  key={field.path}
                  id={`${setting.id}-${field.path}`}
                  field={{ path: field.path, label: field.label, kind }}
                  hint={hint}
                  value={values[field.path]}
                  onChange={(value) => setValues((current) => ({ ...current, [field.path]: value }))}
                />
              )
            })
          )}
          {message !== null ? (
            <p className="text-destructive" role="alert">
              {message}
            </p>
          ) : null}
          <Button className={actionClass} type="submit" disabled={save.isPending}>
            {save.isPending ? 'Saving…' : 'Save'}
          </Button>
        </form>
      </CardContent>
    </Card>
  )
}

function stringValue(value: string | boolean | undefined): string {
  return typeof value === 'string' ? value : ''
}

import { useState, type FormEvent } from 'react'
import { createPortal } from 'react-dom'
import { actionClass, areaClass } from '@/components/classes'
import { FieldControl } from '@/components/FieldControl'
import { FieldInfo } from '@/components/FieldInfo'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { useSaveSetting } from '@/hooks/useSettings'
import { errorMessage } from '@/lib/errors'
import { cn } from '@/lib/utils'
import {
  blockFor,
  fieldValues,
  isRestartRequired,
  rawSettingsValue,
  settingsValue,
  type FieldValues,
  type Setting,
  type SettingsField,
} from '@/lib/settingsForm'

const GROUP_TITLES: Record<string, string> = {
  test: 'Test',
  external: 'External',
  tls: 'TLS',
}

export function SettingsBlock({ setting, actionsHost }: { setting: Setting; actionsHost: HTMLDivElement | null }) {
  const block = blockFor(setting.key)
  const save = useSaveSetting()
  const baseline = block === null ? { raw: JSON.stringify(setting.value, null, 2) } : fieldValues(setting.value, block)
  const [values, setValues] = useState<FieldValues>(baseline)
  const [seen, setSeen] = useState(baseline)
  const [invalid, setInvalid] = useState<string | null>(null)
  if (!sameValues(baseline, seen)) {
    setSeen(baseline)
    if (sameValues(values, seen)) setValues(baseline)
  }
  const dirty = !sameValues(values, baseline)
  const message = invalid ?? (save.error === null ? null : errorMessage(save.error))
  const saved = save.isSuccess && !dirty && message === null

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

  function discard() {
    setValues(baseline)
    setInvalid(null)
    save.reset()
  }

  const formId = `settings-${setting.id}`
  const fieldSections = block === null ? [] : sections(block.fields)
  const split = fieldSections.length > 1
  const actions =
    actionsHost === null
      ? null
      : createPortal(
          <>
            {message !== null ? (
              <p className="max-w-48 truncate text-sm text-destructive" role="alert">
                {message}
              </p>
            ) : null}
            {saved ? <p className="text-sm text-muted-foreground">Saved</p> : null}
            {dirty ? (
              <Button type="button" variant="ghost" className={actionClass} onClick={discard}>
                Discard
              </Button>
            ) : null}
            <Button className={actionClass} type="submit" form={formId} disabled={!dirty || save.isPending}>
              {save.isPending ? 'Saving…' : 'Save'}
            </Button>
          </>,
          actionsHost,
        )

  return (
    <form id={formId} className="flex min-h-0 flex-1 flex-col" onSubmit={onSubmit}>
      {actions}
      <div className="flex-1 overflow-y-auto px-6 py-6">
        <div className="grid gap-8">
          {block?.hint !== undefined ? <p className="text-sm text-muted-foreground">{block.hint}</p> : null}
          {block === null ? (
            <div className="grid gap-2">
              <Label htmlFor={`${setting.id}-raw`}>Value</Label>
              <Textarea
                id={`${setting.id}-raw`}
                className={areaClass}
                spellCheck={false}
                value={stringValue(values.raw)}
                onChange={(event) => setValues({ raw: event.target.value })}
              />
            </div>
          ) : (
            fieldSections.map((section) => (
              <section
                key={section.title ?? 'fields'}
                className={cn(
                  'grid gap-x-8 gap-y-4 md:grid-cols-2 xl:grid-cols-3',
                  split && 'rounded-2xl border border-border bg-muted/60 p-5',
                )}
              >
                {section.title !== null ? (
                  <h2 className="text-sm font-semibold md:col-span-2 xl:col-span-3">{section.title}</h2>
                ) : null}
                {section.fields.map((field) => {
                  const hint = isRestartRequired(block.key, field.path) ? 'Restart required' : undefined
                  const { kind } = field
                  if (kind === 'json') {
                    return (
                      <div key={field.path} className="grid gap-2 md:col-span-2 xl:col-span-3">
                        <span className="flex items-center gap-1.5">
                          <Label htmlFor={`${setting.id}-${field.path}`}>
                            {field.label}
                            {hint ? <span className="ml-2 font-normal text-muted-foreground">{hint}</span> : null}
                          </Label>
                          {field.help !== undefined && field.help !== '' ? <FieldInfo text={field.help} /> : null}
                        </span>
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
                      info={field.help}
                      masked={field.path === 'password'}
                      value={values[field.path]}
                      onChange={(value) => setValues((current) => ({ ...current, [field.path]: value }))}
                    />
                  )
                })}
              </section>
            ))
          )}
        </div>
      </div>
    </form>
  )
}

function sections(fields: readonly SettingsField[]): { title: string | null; fields: SettingsField[] }[] {
  const groups = new Map<string, SettingsField[]>()
  for (const field of fields) {
    const dot = field.path.indexOf('.')
    const key = dot === -1 ? '' : field.path.slice(0, dot)
    const list = groups.get(key) ?? []
    list.push(field)
    groups.set(key, list)
  }
  if (groups.size < 2) return [{ title: null, fields: [...fields] }]
  const out: { title: string | null; fields: SettingsField[] }[] = []
  const top = groups.get('')
  if (top !== undefined) out.push({ title: null, fields: top })
  for (const [key, groupFields] of groups) {
    if (key === '') continue
    out.push({ title: GROUP_TITLES[key] ?? key, fields: groupFields })
  }
  return out
}

function sameValues(a: FieldValues, b: FieldValues): boolean {
  const keys = new Set([...Object.keys(a), ...Object.keys(b)])
  for (const key of keys) {
    if (a[key] !== b[key]) return false
  }
  return true
}

function stringValue(value: string | boolean | undefined): string {
  return typeof value === 'string' ? value : ''
}

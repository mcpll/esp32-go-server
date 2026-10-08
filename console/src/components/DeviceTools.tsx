import { useState, type FormEvent } from 'react'
import { actionClass, areaClass } from '@/components/classes'
import { CommandStatus } from '@/components/CommandStatus'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { useCommand } from '@/hooks/useCommand'
import {
  commandBody,
  mcpCallPayload,
  mcpToolsPayload,
  readCallResult,
  readToolList,
  type DeviceTool,
} from '@/lib/commands'
import { errorMessage } from '@/lib/errors'
import { asJsonObject, type JsonObject } from '@/lib/json'
import type { Device } from '@/lib/records'
import { cn } from '@/lib/utils'

export function DeviceTools({ device }: { device: Device }) {
  const list = useCommand()
  const call = useCommand()
  const [chosen, setChosen] = useState<string | null>(null)
  const [argumentsText, setArgumentsText] = useState('{}')
  const [formError, setFormError] = useState<string | null>(null)
  const listed = toolsFrom(list.command?.status === 'done' ? list.command.result : null)
  const tools = Array.isArray(listed) ? listed : null
  const toolsError = listed instanceof Error ? listed.message : null
  const selected = tools?.find((tool) => tool.name === chosen) ?? tools?.[0] ?? null
  const callText = callTextFrom(call.command?.status === 'done' ? call.command.result : null)
  const listBusy =
    list.sending || list.command?.status === 'pending' || list.command?.status === 'running'
  const callBusy =
    call.sending || call.command?.status === 'pending' || call.command?.status === 'running'
  const argumentsId = `arguments-${device.id}`

  function onList() {
    setFormError(null)
    void list.send(
      commandBody({
        type: 'mcp_tools',
        deviceId: device.id,
        payload: mcpToolsPayload(device.deviceId),
      }),
    )
  }

  function onCall(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (selected === null) return
    setFormError(null)
    let args: JsonObject
    try {
      args = asJsonObject(parseJson(argumentsText), 'Arguments')
    } catch {
      setFormError('Arguments must be a JSON object')
      return
    }
    void call.send(
      commandBody({
        type: 'mcp_call',
        deviceId: device.id,
        payload: mcpCallPayload(device.deviceId, selected.name, args),
      }),
    )
  }

  return (
    <div className="grid gap-3 border-t border-border pt-3">
      <div className="flex items-center justify-between gap-3">
        <p className="text-sm font-medium">Tools</p>
        <Button className={actionClass} type="button" disabled={listBusy} onClick={onList}>
          {listBusy ? 'Listing…' : 'List tools'}
        </Button>
      </div>
      <CommandStatus command={list.command} error={toolsError ?? list.error} />
      {tools !== null && tools.length === 0 ? (
        <p className="text-sm text-muted-foreground">This device has not reported any tools.</p>
      ) : null}
      {tools !== null && tools.length > 0 ? (
        <ul className="grid gap-2">
          {tools.map((tool) => (
            <li key={tool.name}>
              <ToolCard
                tool={tool}
                selected={selected?.name === tool.name}
                onSelect={() => setChosen(tool.name)}
              />
            </li>
          ))}
        </ul>
      ) : null}
      {selected !== null ? (
        <form className="grid gap-3" onSubmit={onCall}>
          <div className="grid gap-2">
            <Label htmlFor={argumentsId}>Arguments for {selected.name}</Label>
            <Textarea
              id={argumentsId}
              className={`${areaClass} min-h-24 font-mono text-sm`}
              value={argumentsText}
              spellCheck={false}
              onChange={(event) => setArgumentsText(event.target.value)}
            />
          </div>
          <div className="flex items-center justify-between gap-3">
            <CommandStatus
              command={call.command}
              error={formError ?? (callText instanceof Error ? callText.message : call.error)}
            />
            <Button className={actionClass} type="submit" disabled={callBusy}>
              {callBusy ? 'Calling…' : 'Call'}
            </Button>
          </div>
          {typeof callText === 'string' ? (
            <pre className={`${areaClass} overflow-x-auto font-mono text-xs whitespace-pre-wrap`}>
              {callText}
            </pre>
          ) : null}
        </form>
      ) : null}
    </div>
  )
}

function ToolCard({
  tool,
  selected,
  onSelect,
}: {
  tool: DeviceTool
  selected: boolean
  onSelect: () => void
}) {
  return (
    <button
      type="button"
      aria-pressed={selected}
      className={cn(
        'grid w-full gap-1 rounded-xl bg-muted/60 px-3 py-2 text-left focus-visible:outline-none focus-visible:ring-3 focus-visible:ring-ring/50',
        selected && 'ring-2 ring-ring',
      )}
      onClick={onSelect}
    >
      <span className="font-mono text-sm">{tool.name}</span>
      <span className="text-sm text-muted-foreground">{tool.description}</span>
      {tool.inputSchema !== null ? (
        <pre className="overflow-x-auto font-mono text-xs text-muted-foreground whitespace-pre-wrap">
          {JSON.stringify(tool.inputSchema, null, 2)}
        </pre>
      ) : null}
    </button>
  )
}

function parseJson(text: string): unknown {
  return JSON.parse(text)
}

function toolsFrom(result: unknown): DeviceTool[] | Error | null {
  if (result === null || result === undefined) return null
  try {
    return readToolList(result)
  } catch (caught) {
    return new Error(errorMessage(caught))
  }
}

function callTextFrom(result: unknown): string | Error | null {
  if (result === null || result === undefined) return null
  try {
    return readCallResult(result)
  } catch (caught) {
    return new Error(errorMessage(caught))
  }
}

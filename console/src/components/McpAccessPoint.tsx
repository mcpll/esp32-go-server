import { useEffect, useState } from 'react'
import { Copy } from 'lucide-react'
import { actionClass } from '@/components/classes'
import { CommandStatus } from '@/components/CommandStatus'
import { Button } from '@/components/ui/button'
import { useCommand } from '@/hooks/useCommand'
import { commandBody, mcpEndpointPayload, readMcpEndpoint, type McpEndpoint } from '@/lib/commands'
import { errorMessage } from '@/lib/errors'
import { cn } from '@/lib/utils'

export function McpAccessPoint({ agentId }: { agentId: string }) {
  const { command, error, sending, send } = useCommand()
  const [copied, setCopied] = useState(false)
  const [copyError, setCopyError] = useState<string | null>(null)

  useEffect(() => {
    void send(
      commandBody({
        type: 'mcp_endpoint',
        agentId,
        payload: mcpEndpointPayload(agentId),
      }),
    )
  }, [agentId, send])

  const endpoint = endpointFrom(command?.status === 'done' ? command.result : null)
  const busy = sending || command?.status === 'pending' || command?.status === 'running'

  async function onCopy() {
    if (!isEndpoint(endpoint)) return
    setCopyError(null)
    try {
      await navigator.clipboard.writeText(endpoint.url)
      setCopied(true)
    } catch (caught) {
      setCopied(false)
      setCopyError(errorMessage(caught))
    }
  }

  function onRefresh() {
    setCopied(false)
    setCopyError(null)
    void send(
      commandBody({
        type: 'mcp_endpoint',
        agentId,
        payload: mcpEndpointPayload(agentId),
      }),
    )
  }

  return (
    <section className="grid gap-3 border-t border-border pt-4">
      <div className="flex items-center justify-between gap-3">
        <h2 className="text-sm font-medium">MCP access point</h2>
        <Button className={actionClass} type="button" disabled={busy} onClick={onRefresh}>
          {busy ? 'Loading…' : 'Refresh'}
        </Button>
      </div>
      <CommandStatus
        command={command}
        error={copyError ?? error ?? (endpoint instanceof Error ? endpoint.message : null)}
      />
      {isEndpoint(endpoint) ? (
        <div className="grid gap-2">
          <p className="font-mono text-xs break-all">{endpoint.url}</p>
          <div className="flex flex-wrap items-center justify-between gap-3">
            <p className="flex items-center gap-1.5 text-sm text-muted-foreground">
              <span
                className={cn(
                  'size-1.5 rounded-full',
                  endpoint.connected ? 'bg-online' : 'bg-muted-foreground/40',
                )}
              />
              {endpoint.connected ? 'Connected' : 'Not connected'},{' '}
              {endpoint.toolsCount === 1 ? '1 tool' : `${endpoint.toolsCount} tools`}
            </p>
            <Button className={actionClass} type="button" onClick={() => void onCopy()}>
              <Copy />
              {copied ? 'Copied' : 'Copy'}
            </Button>
          </div>
        </div>
      ) : null}
    </section>
  )
}

function endpointFrom(result: unknown): McpEndpoint | Error | null {
  if (result === null || result === undefined) return null
  try {
    return readMcpEndpoint(result)
  } catch (caught) {
    return new Error(errorMessage(caught))
  }
}

function isEndpoint(value: McpEndpoint | Error | null): value is McpEndpoint {
  return value !== null && !(value instanceof Error)
}

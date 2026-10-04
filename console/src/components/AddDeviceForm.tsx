import { useState, type FormEvent, type ReactNode } from 'react'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { useBindDevice } from '@/hooks/useDevices'
import { errorMessage } from '@/lib/errors'
import type { Agent } from '@/lib/records'

export function AddDeviceForm({ agents }: { agents: Agent[] }) {
  const bind = useBindDevice()
  const [code, setCode] = useState('')
  const [agentId, setAgentId] = useState(agents[0]?.id ?? '')
  const [note, setNote] = useState('')
  const message = bind.error === null ? null : errorMessage(bind.error)
  const items: Record<string, ReactNode> = {}
  for (const agent of agents) items[agent.id] = agent.name

  function onSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    bind.mutate(
      { code, agentId, note },
      {
        onSuccess: () => {
          setCode('')
          setNote('')
        },
      },
    )
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>Add device</CardTitle>
      </CardHeader>
      <CardContent>
        {agents.length === 0 ? (
          <p>No agents yet.</p>
        ) : (
          <form className="grid gap-4" onSubmit={onSubmit}>
            <div className="grid gap-2">
              <Label htmlFor="device-code">Six-digit code</Label>
              <Input
                id="device-code"
                inputMode="numeric"
                autoComplete="off"
                value={code}
                onChange={(event) => setCode(event.target.value)}
              />
            </div>
            <div className="grid gap-2">
              <Label htmlFor="device-agent">Agent</Label>
              <Select items={items} value={agentId} onValueChange={(value) => setAgentId(value ?? '')}>
                <SelectTrigger id="device-agent" className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {agents.map((agent) => (
                    <SelectItem key={agent.id} value={agent.id}>
                      {agent.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="grid gap-2">
              <Label htmlFor="device-note">Note</Label>
              <Input id="device-note" value={note} onChange={(event) => setNote(event.target.value)} />
            </div>
            <p className="text-sm text-muted-foreground">
              The device leaves the activation screen on its next check-in.
            </p>
            {message !== null ? (
              <p className="text-destructive" role="alert">
                {message}
              </p>
            ) : null}
            <Button type="submit" disabled={bind.isPending}>
              {bind.isPending ? 'Adding…' : 'Add device'}
            </Button>
          </form>
        )}
      </CardContent>
    </Card>
  )
}

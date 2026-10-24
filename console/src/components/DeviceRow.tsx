import { DeviceTools } from '@/components/DeviceTools'
import { SendMessage } from '@/components/SendMessage'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { useSetDeviceActivated } from '@/hooks/useDevices'
import { errorMessage } from '@/lib/errors'
import type { Device } from '@/lib/records'
import { cn } from '@/lib/utils'

export function DeviceRow({ device }: { device: Device }) {
  const activate = useSetDeviceActivated()
  const message = activate.error === null ? null : errorMessage(activate.error)
  const switchId = `activated-${device.id}`

  return (
    <div className="grid gap-3 rounded-2xl border border-border bg-muted/50 px-4 py-3">
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <p className="font-mono text-sm">{device.code}</p>
        <p className="flex items-center gap-1.5 text-sm text-muted-foreground">
          <span
            className={cn('size-1.5 rounded-full', device.online ? 'bg-online' : 'bg-muted-foreground/40')}
          />
          {device.online ? 'Online' : 'Offline'}
        </p>
      </div>
      <p className="text-sm">{device.agentName !== '' ? device.agentName : 'No agent'}</p>
      {device.note !== '' ? <p className="text-sm text-muted-foreground">{device.note}</p> : null}
      <div className="flex items-center justify-between gap-4">
        <Label htmlFor={switchId}>Activated</Label>
        <Switch
          id={switchId}
          checked={device.activated}
          disabled={activate.isPending}
          onCheckedChange={(checked) =>
            activate.mutate({ id: device.id, activated: checked, agentId: device.agentId })
          }
        />
      </div>
      {message !== null ? (
        <p className="text-sm text-destructive" role="alert">
          {message}
        </p>
      ) : null}
      <SendMessage device={device} />
      <DeviceTools device={device} />
    </div>
  )
}

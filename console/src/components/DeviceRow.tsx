import { Card, CardContent } from '@/components/ui/card'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { useSetDeviceActivated } from '@/hooks/useDevices'
import { errorMessage } from '@/lib/errors'
import type { Device } from '@/lib/records'

export function DeviceRow({ device }: { device: Device }) {
  const activate = useSetDeviceActivated()
  const message = activate.error === null ? null : errorMessage(activate.error)
  const switchId = `activated-${device.id}`

  return (
    <Card>
      <CardContent className="grid gap-3">
        <div className="flex flex-wrap items-baseline justify-between gap-2">
          <p className="font-mono text-base">{device.code}</p>
          <p className="text-sm text-muted-foreground">{device.online ? 'Online' : 'Offline'}</p>
        </div>
        <p>{device.agentName !== '' ? device.agentName : 'No agent'}</p>
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
          <p className="text-destructive" role="alert">
            {message}
          </p>
        ) : null}
      </CardContent>
    </Card>
  )
}

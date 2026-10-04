import { AddDeviceForm } from '@/components/AddDeviceForm'
import { DeviceRow } from '@/components/DeviceRow'
import { useDeviceScreen } from '@/hooks/useDevices'

export function DevicesPage() {
  const { data } = useDeviceScreen()
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <header className="flex h-14 shrink-0 items-center border-b border-border px-4">
        <h1 className="text-sm font-semibold">Devices</h1>
      </header>
      <div className="flex-1 overflow-y-auto px-6 py-6">
        <div className="grid max-w-lg gap-5">
          <AddDeviceForm agents={data.agents} />
          {data.devices.length === 0 ? (
            <p className="text-sm text-muted-foreground">
              No devices yet. A device shows a six-digit code until you add it here.
            </p>
          ) : (
            <ul className="grid gap-2">
              {data.devices.map((device) => (
                <li key={device.id}>
                  <DeviceRow device={device} />
                </li>
              ))}
            </ul>
          )}
        </div>
      </div>
    </div>
  )
}

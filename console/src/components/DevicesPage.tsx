import { AddDeviceForm } from '@/components/AddDeviceForm'
import { DeviceRow } from '@/components/DeviceRow'
import { useDeviceScreen } from '@/hooks/useDevices'

export function DevicesPage() {
  const { data } = useDeviceScreen()
  return (
    <section className="grid gap-6">
      <h1 className="text-xl font-semibold">Devices</h1>
      <AddDeviceForm agents={data.agents} />
      {data.devices.length === 0 ? (
        <p>No devices yet. A device shows a six-digit code until you add it here.</p>
      ) : (
        <ul className="grid gap-3">
          {data.devices.map((device) => (
            <li key={device.id}>
              <DeviceRow device={device} />
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}

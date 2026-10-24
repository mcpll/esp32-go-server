import type { Device } from '@/lib/records'

const runningWarning =
  ' The hardware may still be running; add it again with a new code.'

export function deviceRemoveConfirmMessage(device: Device): string {
  const label = device.note.trim() !== '' ? device.note.trim() : `device ${device.code}`
  let message = `Remove ${label}? This cannot be undone.`
  if (device.online || device.activated) message += runningWarning
  return message
}

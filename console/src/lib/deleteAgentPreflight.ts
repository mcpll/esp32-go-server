export function devicesBlockMessage(deviceCount: number): string {
  if (deviceCount <= 0) throw new Error('devicesBlockMessage requires a positive count')
  if (deviceCount === 1) {
    return 'One device still uses this agent. Reassign or remove it before deleting.'
  }
  return `${deviceCount} devices still use this agent. Reassign or remove them before deleting.`
}

export function assertNoDevicesForAgent(deviceCount: number): void {
  if (deviceCount > 0) throw new Error(devicesBlockMessage(deviceCount))
}

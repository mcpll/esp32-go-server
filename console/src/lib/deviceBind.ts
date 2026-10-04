export function parseDeviceCode(input: string): string {
  const code = input.trim()
  if (!/^[0-9]{6}$/.test(code)) {
    throw new Error('Enter the six-digit code from the device')
  }
  return code
}

export function bindDeviceUpdate(
  agentId: string,
  note: string,
): { agent: string; note: string; activated: true } {
  if (agentId === '') throw new Error('Pick an agent')
  return { agent: agentId, note: note.trim(), activated: true }
}

export function activationUpdate(
  activated: boolean,
  agentId: string,
): { activated: boolean } {
  if (activated && agentId === '') {
    throw new Error('Pick an agent before turning activation on')
  }
  return { activated }
}

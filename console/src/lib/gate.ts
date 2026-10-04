export const LOGIN_PATH = '/login'
export const AGENTS_PATH = '/agents'
export const DEVICES_PATH = '/devices'

export function isReachable(loggedIn: boolean, path: string): boolean {
  if (!loggedIn) return path === LOGIN_PATH
  if (path === LOGIN_PATH) return false
  if (path === AGENTS_PATH || path === DEVICES_PATH) return true
  return path.startsWith(`${AGENTS_PATH}/`)
}

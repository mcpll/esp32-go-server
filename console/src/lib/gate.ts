export const LOGIN_PATH = '/login'
export const AGENTS_PATH = '/agents'
export const DEVICES_PATH = '/devices'
export const SETTINGS_PATH = '/settings'
export const STATUS_PATH = '/status'

export function isReachable(loggedIn: boolean, path: string): boolean {
  if (!loggedIn) return path === LOGIN_PATH
  if (path === LOGIN_PATH) return false
  if (
    path === AGENTS_PATH ||
    path === DEVICES_PATH ||
    path === SETTINGS_PATH ||
    path === STATUS_PATH
  ) {
    return true
  }
  return path.startsWith(`${AGENTS_PATH}/`)
}

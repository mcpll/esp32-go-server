import { useLoggedIn } from '@/hooks/useLoggedIn'
import { GuestApp } from '@/GuestApp'
import { MemberApp } from '@/MemberApp'

export function App() {
  const loggedIn = useLoggedIn()
  return loggedIn ? <MemberApp /> : <GuestApp />
}

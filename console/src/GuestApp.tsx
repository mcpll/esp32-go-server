import { Navigate, Route, Routes } from 'react-router-dom'
import { LoginPage } from '@/components/LoginPage'
import { LOGIN_PATH } from '@/lib/gate'

export function GuestApp() {
  return (
    <Routes>
      <Route path={LOGIN_PATH} element={<LoginPage />} />
      <Route path="*" element={<Navigate to={LOGIN_PATH} replace />} />
    </Routes>
  )
}

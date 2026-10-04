import { Navigate, Route, Routes } from 'react-router-dom'
import { AgentEditor } from '@/components/AgentEditor'
import { AgentsPage } from '@/components/AgentsPage'
import { DevicesPage } from '@/components/DevicesPage'
import { SettingsPage } from '@/components/SettingsPage'
import { Shell } from '@/components/Shell'
import { AGENTS_PATH, DEVICES_PATH, LOGIN_PATH, SETTINGS_PATH } from '@/lib/gate'
import { SessionProvider } from '@/session/SessionProvider'

export function MemberApp() {
  return (
    <SessionProvider>
      <Routes>
        <Route path={LOGIN_PATH} element={<Navigate to={AGENTS_PATH} replace />} />
        <Route element={<Shell />}>
          <Route path={AGENTS_PATH} element={<AgentsPage />} />
          <Route path={`${AGENTS_PATH}/:id`} element={<AgentEditor />} />
          <Route path={DEVICES_PATH} element={<DevicesPage />} />
          <Route path={SETTINGS_PATH} element={<SettingsPage />} />
          <Route path="*" element={<Navigate to={AGENTS_PATH} replace />} />
        </Route>
      </Routes>
    </SessionProvider>
  )
}

import { useLayoutEffect, type ReactNode } from 'react'
import { NavLink, Outlet, useLocation } from 'react-router-dom'
import { useApplicationUiPreferences } from '@/shared/state/application-ui-store'
import { useConnectionStore } from '@/shared/realtime/connection-store'
import { HardRefreshButton } from './HardRefreshButton'

const navigation = [
  { to: '/dashboard', label: 'Dashboard', end: true },
  { to: '/absence-dashboard', label: 'Absence alerts', end: false },
  { to: '/courses', label: 'All courses', end: false },
] as const

export function AppShell() {
  const { pathname } = useLocation()

  useLayoutEffect(() => {
    window.scrollTo(0, 0)
  }, [pathname])

  return (
    <AppShellFrame>
      <Outlet />
    </AppShellFrame>
  )
}

export function AppShellFrame({ children }: { readonly children: ReactNode }) {
  const { density, setDensity } = useApplicationUiPreferences()
  const connectionStatus = useConnectionStore((state) => state.status)

  return (
    <div className="app-shell" data-density={density}>
      <a className="skip-link" href="#main-content">
        Skip to content
      </a>
      <header className="app-topbar">
        <NavLink aria-label="Check-in Command Center home" className="app-brand" to="/dashboard">
          <span className="app-brand__mark" aria-hidden="true">C</span>
          <span className="app-brand__name">Check-in Center</span>
        </NavLink>
        <nav aria-label="Primary" className="app-nav">
          {navigation.map((item) => (
            <NavLink
              className={({ isActive }) => `app-nav__link${isActive ? ' is-active' : ''}`}
              end={item.end}
              key={item.to}
              to={item.to}
            >
              {item.label}
            </NavLink>
          ))}
        </nav>
        <div className="app-topbar__utilities">
          <HardRefreshButton />
          <label className="density-control">
            <span>Density</span>
            <select
              value={density}
              onChange={(event) =>
                setDensity(event.target.value === 'compact' ? 'compact' : 'comfortable')
              }
            >
              <option value="comfortable">Comfortable</option>
              <option value="compact">Compact</option>
            </select>
          </label>
          <span className="connection-status" data-status={connectionStatus}>
            <span aria-hidden="true" />
            {connectionStatus === 'connected' ? 'Live' : 'Reconnecting'}
          </span>
        </div>
      </header>
      {connectionStatus === 'offline' && (
        <div className="offline-banner" role="status">
          Live updates are unavailable. Data will refresh every 10 seconds.
        </div>
      )}
      <main className="app-main" id="main-content">
        {children}
      </main>
    </div>
  )
}

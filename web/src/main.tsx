if (import.meta.env.DEV && import.meta.env['VITE_ENABLE_REACT_DEBUG_TOOLS'] === 'true') {
  void import('react-grab')
  void import('react-scan')
}

import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { RouterProvider } from 'react-router-dom'
import { AppProviders } from '@/app/providers'
import { router } from '@/app/router'
import {
  browserAppVersionRuntime,
  checkForAppUpdate,
} from '@/app/app-version'
import '@/shared/styles/index.css'

const rootElement = document.getElementById('root')

if (rootElement === null) {
  throw new Error('Application root element was not found')
}

async function bootstrap(applicationRoot: HTMLElement): Promise<void> {
  const initialVersion = await checkForAppUpdate(browserAppVersionRuntime)
  if (initialVersion === 'reloading') {
    return
  }

  createRoot(applicationRoot).render(
    <StrictMode>
      <AppProviders>
        <RouterProvider router={router} />
      </AppProviders>
    </StrictMode>,
  )

  const checkLatestVersion = () => {
    void checkForAppUpdate(browserAppVersionRuntime)
  }
  window.addEventListener('pageshow', (event) => {
    if (event.persisted) {
      checkLatestVersion()
    }
  })
  document.addEventListener('visibilitychange', () => {
    if (document.visibilityState === 'visible') {
      checkLatestVersion()
    }
  })
}

void bootstrap(rootElement)

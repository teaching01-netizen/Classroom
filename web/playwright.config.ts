import os from 'node:os'
import { defineConfig, devices } from '@playwright/test'

const darwinMajor = Number.parseInt(os.release().split('.')[0] ?? '', 10)
// Playwright 1.62 cannot drive its frozen WebKit build on macOS 14.
const supportsCurrentWebKit = process.platform !== 'darwin' || darwinMajor >= 24

const touchProjects = [
  {
    name: 'iphone-touch-chrome',
    testMatch: /session-row-navigation\.spec\.ts/,
    use: { ...devices['iPhone 15'], browserName: 'chromium' as const },
  },
  {
    name: 'ipad-touch-chrome',
    testMatch: /session-row-navigation\.spec\.ts/,
    use: { ...devices['iPad Pro 11'], browserName: 'chromium' as const },
  },
  ...(supportsCurrentWebKit ? [
    {
      name: 'iphone-webkit',
      testMatch: /session-row-navigation\.spec\.ts/,
      use: { ...devices['iPhone 15'] },
    },
    {
      name: 'ipad-webkit',
      testMatch: /session-row-navigation\.spec\.ts/,
      use: { ...devices['iPad Pro 11'] },
    },
  ] : []),
]

export default defineConfig({
  testDir: './e2e',
  fullyParallel: true,
  retries: 0,
  reporter: 'list',
  use: {
    baseURL: 'http://127.0.0.1:4175',
    screenshot: { mode: 'only-on-failure', fullPage: true },
    trace: 'retain-on-failure',
  },
  projects: [
    {
      name: 'desktop-chrome',
      use: { ...devices['Desktop Chrome'], channel: 'chrome' },
    },
    ...touchProjects,
  ],
  webServer: {
    command: 'npm run dev -- --host 127.0.0.1 --port 4175',
    url: 'http://127.0.0.1:4175',
    reuseExistingServer: false,
    stdout: 'ignore',
    stderr: 'pipe',
  },
})

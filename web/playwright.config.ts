import { defineConfig, devices } from '@playwright/test'

const baseURL = process.env.GLEAN_E2E_BASE_URL || 'http://127.0.0.1:18081'
const live = process.env.GLEAN_E2E_PHASE === 'live'
const phase = live ? 'live' : 'historical'
const target = new URL(baseURL)
if (target.hostname !== '127.0.0.1' || target.protocol !== 'http:') {
  throw new Error('Browser acceptance tests require the dedicated loopback HTTP environment')
}

export default defineConfig({
  testDir: './e2e',
  testMatch: live ? 'live-observation.spec.ts' : 'catalog.spec.ts',
  fullyParallel: false,
  workers: 1,
  retries: 0,
  forbidOnly: !!process.env.CI,
  timeout: 120_000,
  globalTimeout: 8 * 60_000,
  expect: { timeout: 15_000 },
  outputDir: `test-results/${phase}`,
  reporter: [['list'], ['html', { outputFolder: `playwright-report/${phase}`, open: 'never' }]],
  use: {
    baseURL,
    actionTimeout: 15_000,
    navigationTimeout: 30_000,
    // Screenshots are useful evidence; traces/HAR/storageState would retain real JWTs.
    trace: 'off',
    video: 'off',
    screenshot: 'only-on-failure',
  },
  projects: [
    {
      name: 'desktop',
      use: { ...devices['Desktop Chrome'], viewport: { width: 1440, height: 1080 } },
    },
    { name: 'mobile', use: { ...devices['Pixel 7'] } },
  ],
  webServer: {
    command: `npm run preview -- --host 127.0.0.1 --port ${target.port || '18081'} --strictPort`,
    url: baseURL,
    reuseExistingServer: !process.env.CI,
    timeout: 30_000,
    env: { GLEAN_API_PROXY: process.env.GLEAN_API_PROXY || 'http://127.0.0.1:18080' },
  },
})

import { defineConfig } from "@playwright/test"

/**
 * E2E against a real bin/proxying started by the test (see e2e/traffic.spec.ts).
 * No webServer here — the binary serves the embedded SPA.
 */
export default defineConfig({
  testDir: "./e2e",
  fullyParallel: false,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  workers: 1,
  timeout: 60_000,
  expect: { timeout: 15_000 },
  use: {
    trace: "on-first-retry",
    headless: true,
  },
  projects: [{ name: "chromium", use: { browserName: "chromium" } }],
})

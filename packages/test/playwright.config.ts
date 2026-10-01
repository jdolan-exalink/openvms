import { defineConfig, devices } from "@playwright/test";

/**
 * Maps performance harness (M-W10). The smoke serves the production build through
 * `vite preview` and stubs every `/api/v1` call in the browser, so the run needs no
 * backend: what is under test is the client render path with 5k synthetic cameras.
 */
export default defineConfig({
  testDir: "./tests",
  timeout: 120_000,
  expect: { timeout: 15_000 },
  fullyParallel: false,
  workers: 1,
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? [["list"], ["html", { open: "never" }]] : [["list"]],
  use: {
    ...devices["Desktop Chrome"],
    baseURL: "http://127.0.0.1:4173",
    launchOptions: {
      // Headless GL runs on SwiftShader; newer Chromium needs this explicit opt-in.
      args: ["--enable-unsafe-swiftshader"],
    },
  },
  webServer: {
    command: "pnpm --filter @openvms/web build && pnpm --filter @openvms/web exec vite preview --port 4173 --strictPort --host 127.0.0.1",
    url: "http://127.0.0.1:4173",
    reuseExistingServer: !process.env.CI,
    timeout: 240_000,
  },
});

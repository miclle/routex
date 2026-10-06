import { defineConfig } from 'vitest/config'

export default defineConfig({
  resolve: {
    alias: {
      '@': '/src',
      src: '/src',
    },
  },
  test: {
    environment: 'jsdom',
    // Let jsdom supply browser storage instead of Node's process-global Web Storage.
    execArgv: ['--no-experimental-webstorage'],
    setupFiles: ['src/i18n/test-setup.ts'],
    // Bound DOM workers so concurrent backend builds do not starve test timers.
    maxWorkers: 2,
    include: ['src/**/*.test.{ts,tsx}'],
  },
})

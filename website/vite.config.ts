import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

const vitePort = Number.parseInt(process.env.ROUTEX_VITE_PORT ?? '5173', 10)
const apiBaseURL =
  process.env.ROUTEX_API_BASE_URL ??
  `http://127.0.0.1:${process.env.ROUTEX_HTTP_PORT ?? '9000'}`

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [react(), tailwindcss()],
  build: {
    outDir: 'build',
  },
  resolve: {
    alias: {
      "@": "/src",
      src: "/src",
    },
  },
  server: {
    port: vitePort,
    strictPort: true,
    host: "0.0.0.0",
    proxy: {
      "/api/v1": apiBaseURL,
    },
  },
})

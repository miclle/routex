import assert from 'node:assert/strict'
import http from 'node:http'
import { mkdtemp, rm } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import test from 'node:test'
import { createServer } from 'vite'

test('development server accepts localhost and rejects untrusted hosts', async (t) => {
  const cacheDir = await mkdtemp(join(tmpdir(), 'routex-vite-hosts-'))
  const server = await createServer({
    cacheDir,
    server: { host: '127.0.0.1', port: 0, strictPort: false },
    optimizeDeps: { noDiscovery: true, include: [] },
  })
  try {
    await server.listen()
    const port = server.httpServer.address().port
    for (const [host, status] of [
      ['localhost', 200],
      ['untrusted.example', 403],
    ]) {
      await t.test(host, async () => {
        const response = await new Promise((resolve, reject) => {
          http
            .get(
              { hostname: '127.0.0.1', port, path: '/src/api/client.ts', headers: { Host: host } },
              (res) => {
                let body = ''
                res.on('data', (chunk) => {
                  body += chunk
                })
                res.on('end', () => resolve({ status: res.statusCode, body }))
                res.on('error', reject)
              },
            )
            .on('error', reject)
        })
        assert.equal(response.status, status)
        assert.equal(response.body.includes('axios.create'), status === 200)
      })
    }
  } finally {
    // Keep the event loop alive while Vite finishes its unreferenced workers.
    const keepAlive = setInterval(() => {}, 100)
    try {
      await server.close()
    } finally {
      clearInterval(keepAlive)
      await rm(cacheDir, { recursive: true, force: true })
    }
  }
})

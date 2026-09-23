import assert from 'node:assert/strict'
import http from 'node:http'
import { mkdtemp, rm } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import test from 'node:test'
import { createServer } from 'vite'

test('development server proxies native model and streaming inference requests', async () => {
  const cacheDir = await mkdtemp(join(tmpdir(), 'routex-vite-proxy-'))
  const observed = []
  const upstream = http.createServer(async (req, res) => {
    observed.push({ path: req.url, authorization: req.headers.authorization })
    if (req.url === '/v1/models') {
      res.writeHead(200, { 'Content-Type': 'application/json' })
      res.end(JSON.stringify({ data: [{ id: 'native-model' }] }))
      return
    }
    let body = ''
    for await (const part of req) body += part
    assert.equal(JSON.parse(body).model, 'native-model')
    res.writeHead(200, { 'Content-Type': 'text/event-stream', 'X-Request-ID': 'req_dev_proxy' })
    res.write('data: {"choices":[{"delta":{"content":"proxied"}}]}\n\n')
    res.end('data: [DONE]\n\n')
  })
  await new Promise((resolve) => upstream.listen(0, '127.0.0.1', resolve))
  const previousOrigin = process.env.ROUTEX_API_BASE_URL
  process.env.ROUTEX_API_BASE_URL = `http://127.0.0.1:${upstream.address().port}`
  let server
  try {
    server = await createServer({ cacheDir, server: { host: '127.0.0.1', port: 0, strictPort: false, hmr: false }, optimizeDeps: { noDiscovery: true, include: [] } })
    await server.listen()
    const origin = `http://127.0.0.1:${server.httpServer.address().port}`
    const headers = { Authorization: 'Bearer disposable-proxy-test' }
    const models = await fetch(`${origin}/v1/models`, { headers })
    assert.equal(models.status, 200)
    assert.deepEqual(await models.json(), { data: [{ id: 'native-model' }] })
    const completion = await fetch(`${origin}/v1/chat/completions`, { method: 'POST', headers: { ...headers, 'Content-Type': 'application/json' }, body: JSON.stringify({ model: 'native-model', messages: [{ role: 'user', content: 'hello' }], stream: true }) })
    assert.equal(completion.headers.get('X-Request-ID'), 'req_dev_proxy')
    assert.match(completion.headers.get('Content-Type'), /text\/event-stream/)
    assert.equal(await completion.text(), 'data: {"choices":[{"delta":{"content":"proxied"}}]}\n\ndata: [DONE]\n\n')
    assert.deepEqual(observed, [{ path: '/v1/models', authorization: headers.Authorization }, { path: '/v1/chat/completions', authorization: headers.Authorization }])
  } finally {
    if (previousOrigin === undefined) delete process.env.ROUTEX_API_BASE_URL
    else process.env.ROUTEX_API_BASE_URL = previousOrigin
    const keepAlive = setInterval(() => {}, 100)
    try { await server?.close(); await new Promise((resolve) => upstream.close(resolve)) } finally { clearInterval(keepAlive); await rm(cacheDir, { recursive: true, force: true }) }
  }
})

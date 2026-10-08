import { beforeEach, afterEach, it, expect } from 'vitest'
import { AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import client from './client'
import { getModelPublicNames } from './model-public-names'
import { searchPublicModelNames } from '@/lib/public-model-references'
const original = client.defaults.adapter
let data: unknown, requests: InternalAxiosRequestConfig[]
beforeEach(() => {
  requests = []
  data = {
    connection_id: 'con_one',
    query: 'gpt',
    items: searchPublicModelNames('gpt').map((name) => ({ name, available: name !== 'gpt-5.2' })),
  }
  client.defaults.adapter = async (config) => {
    requests.push(config)
    return { config, status: 200, statusText: '', headers: new AxiosHeaders(), data }
  }
})
afterEach(() => {
  client.defaults.adapter = original
})
it('keeps exact candidate order and reservation state, passing cancellation without secret or write authority', async () => {
  const signal = new AbortController().signal
  const result = await getModelPublicNames('con_one', 'gpt', signal)
  expect(result.items.map((x) => x.available)).toEqual([false, true])
  expect(requests[0].url).toBe('/admin/connections/con_one/model-creation/public-names')
  expect(requests[0].params).toEqual({ q: 'gpt' })
  expect(requests[0].signal).toBe(signal)
  expect(requests[0].method).toBe('get')
  expect(requests[0].headers.has('X-CSRF-Token')).toBe(false)
})
it.each(['actor', 'name', 'cursor', 'provider', 'price'])(
  'rejects foreign %s response facts',
  async (extra) => {
    data = { ...(data as object), [extra]: 'private' }
    await expect(getModelPublicNames('con_one', 'gpt')).rejects.toThrow(
      'Invalid public name response',
    )
  },
)
it('rejects omissions, aliases, duplicates, reordered rows and unknown availability instead of inferring free', async () => {
  const valid = structuredClone(data) as {
    connection_id: string
    query: string
    items: { name: string; available: boolean }[]
  }
  for (const bad of [
    { ...valid, connection_id: 'con_other' },
    { ...valid, query: 'GPT' },
    { ...valid, items: valid.items.slice(0, 1) },
    { ...valid, items: [valid.items[0], valid.items[0]] },
    { ...valid, items: [...valid.items].reverse() },
    { ...valid, items: [{ name: 'private-name', available: true }, valid.items[1]] },
    { ...valid, items: [{ ...valid.items[0], available: null }, valid.items[1]] },
    { ...valid, items: [{ ...valid.items[0], owner: 'mdl_private' }, valid.items[1]] },
  ]) {
    data = bad
    await expect(getModelPublicNames('con_one', 'gpt')).rejects.toThrow()
  }
})
it('rejects malformed identity/query before any request, but empty/literal custom queries are read-only', async () => {
  for (const [id, q] of [
    ['CON_one', 'gpt'],
    ['con_one ', 'gpt'],
    ['con_one', 'x'.repeat(129)],
    ['con_one', 'a\n'],
    ['con_one', '\ud800'],
  ])
    await expect(getModelPublicNames(id, q)).rejects.toThrow()
  expect(requests).toHaveLength(0)
  data = { connection_id: 'con_one', query: '_%!', items: [] }
  expect((await getModelPublicNames('con_one', '_%!')).items).toEqual([])
})

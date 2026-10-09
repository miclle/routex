import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { AxiosHeaders } from 'axios'
import client from './client'
import { getAdminModel, listAdminModels } from './catalog'

const originalAdapter = client.defaults.adapter
let value: unknown
function model() {
  return {
    id: 'mdl_one',
    name: 'Configured Model',
    status: 'active',
    names: [{ name: 'Configured Model', is_current: true, expires_at: null }],
    bindings: [],
    granted_user_ids: [],
  }
}
beforeEach(() => {
  client.defaults.adapter = async (config) => ({
    config,
    data: structuredClone(value),
    headers: new AxiosHeaders(),
    status: 200,
    statusText: '',
  })
})
afterEach(() => {
  client.defaults.adapter = originalAdapter
})

describe('Administrative Model configured availability wire boundary', () => {
  it.each([true, false, null])(
    'preserves configured_ready=%j in list and detail',
    async (ready) => {
      const record = { ...model(), configured_ready: ready }
      value = record
      expect((await getAdminModel('mdl_one')).configured_ready).toBe(ready)
      value = { items: [record] }
      expect((await listAdminModels())[0].configured_ready).toBe(ready)
    },
  )
  it('keeps legacy absence unknown rather than deriving readiness from empty bindings', async () => {
    value = model()
    expect((await getAdminModel('mdl_one')).configured_ready).toBeUndefined()
    value = { items: [model()] }
    expect((await listAdminModels())[0].configured_ready).toBeUndefined()
  })
  it.each([undefined, 0, 1, 'true', 'false', 'unknown', [], {}])(
    'rejects present malformed configured_ready=%j in the complete list and detail',
    async (ready) => {
      const invalid = { ...model(), configured_ready: ready }
      value = invalid
      await expect(getAdminModel('mdl_one')).rejects.toThrow(
        'Invalid Model configured availability',
      )
      value = { items: [{ ...model(), configured_ready: true }, invalid] }
      await expect(listAdminModels()).rejects.toThrow('Invalid Model configured availability')
    },
  )
})

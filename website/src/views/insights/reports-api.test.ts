import { afterEach, expect, it } from 'vitest'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import client from '@/api/client'
import {
  deleteInsightsReport,
  getInsightsReports,
  parseInsightsReport,
  parseInsightsReportPage,
  saveInsightsReport,
} from '@/api/insights-reports'
import type { InsightsReportDefinition, InsightsReportInput } from '@/types/insights'

const original = client.defaults.adapter
const id = 'isr_01arz3ndektsv4rrffq69g5fav'
const input: InsightsReportInput = {
  request_id: '6ff6cfe1-704c-4fbb-8684-65d02535188c',
  kind: 'tokens_by_model',
  from: '2026-10-01T08:00:00.123456789+08:00',
  to: '2026-10-01T08:00:00.123456790+08:00',
  timezone: 'Asia/Shanghai',
}
const definition = (): InsightsReportDefinition => ({
  id,
  kind: input.kind,
  from: '2026-10-01T00:00:00.123456789Z',
  to: '2026-10-01T00:00:00.12345679Z',
  timezone: input.timezone,
  created_at: '2026-10-02T00:00:00.123456Z',
})
const signal = () => new AbortController().signal
const response = (config: InternalAxiosRequestConfig, data: unknown, status = 200) => ({
  config,
  data,
  status,
  statusText: '',
  headers: new AxiosHeaders(),
})
afterEach(() => {
  client.defaults.adapter = original
})
it('accepts exact owner definitions, positive nanosecond ranges and ordered opaque-cursor pages', async () => {
  expect(parseInsightsReport(definition())).toEqual(definition())
  const older = { ...definition(), id: 'isr_01arz3ndektsv4rrffq69g5fat' }
  expect(
    parseInsightsReportPage(
      { items: [definition(), older], next_cursor: 'opaque-current-page_123' },
      20,
    ).items,
  ).toHaveLength(2)
  expect(parseInsightsReportPage({ items: [], next_cursor: '' }, 20)).toEqual({
    items: [],
    next_cursor: '',
  })
  const requests: InternalAxiosRequestConfig[] = []
  client.defaults.adapter = async (config) => {
    requests.push(config)
    return response(
      config,
      config.method === 'post'
        ? definition()
        : { items: [definition()], next_cursor: 'opaque_next' },
      config.method === 'post' ? 201 : 200,
    )
  }
  await expect(saveInsightsReport(input, 'csrf', signal())).resolves.toEqual(definition())
  expect(JSON.parse(requests[0].data)).toEqual(input)
  expect(requests[0].headers.get('X-CSRF-Token')).toBe('csrf')
  await getInsightsReports(signal(), 'opaque-current-page_123', 100)
  expect(requests[1].params).toEqual({ limit: 100, cursor: 'opaque-current-page_123' })
  await getInsightsReports(signal())
  expect(requests[2].params).toEqual({ limit: 20 })
})
it('rejects extra private fields, noncanonical IDs, invalid calendars and incomplete or unordered pages', () => {
  for (const mutation of [
    { id: id.toUpperCase() },
    { id: 'isr_81arz3ndektsv4rrffq69g5fav' },
    { kind: 'forecast' },
    { from: '2026-02-30T00:00:00Z' },
    { from: '2026-01-01T24:00:00Z' },
    { from: '2026-10-01T00:00:00.123456789Z', to: '2026-10-01T00:00:00.123456789Z' },
    { from: '2026-10-01T08:00:00.123456789+08:00' },
    { timezone: 'Local' },
    { timezone: '+01:00' },
    { created_at: '2026-02-29T00:00:00Z' },
    { statistics: {} },
    { question: 'private' },
    { owner_id: 'another' },
    { ai_text: 'private' },
  ])
    expect(() => parseInsightsReport({ ...definition(), ...mutation })).toThrow()
  for (const page of [
    { items: [definition(), definition()], next_cursor: '' },
    {
      items: [{ ...definition(), id: 'isr_01arz3ndektsv4rrffq69g5fat' }, definition()],
      next_cursor: '',
    },
    { items: [], next_cursor: 'unexpected' },
    { items: [], next_cursor: null },
    { items: [definition()], next_cursor: 'x'.repeat(161) },
    { items: [], next_cursor: '', total: 0 },
  ])
    expect(() => parseInsightsReportPage(page, 20)).toThrow()
  expect(() => parseInsightsReportPage({ items: [definition()], next_cursor: '' }, 0)).toThrow()
})
it('rejects invalid outbound intents and query bounds before any dispatch', async () => {
  let calls = 0
  client.defaults.adapter = async (config) => {
    calls++
    return response(config, definition(), 201)
  }
  for (const mutation of [
    { request_id: input.request_id.toUpperCase() },
    { request_id: '6ff6cfe1-704c-1fbb-8684-65d02535188c' },
    { kind: 'arbitrary' },
    { from: '2026-02-30T00:00:00Z' },
    { to: '2028-10-01T00:00:00Z' },
    { from: '2026-10-01T00:00:00Z', to: '2026-10-01T00:00:00Z' },
    { timezone: 'Local' },
    { timezone: '+08:00' },
    { question: 'private' },
    { request_id: null },
  ])
    await expect(
      saveInsightsReport({ ...input, ...mutation } as InsightsReportInput, 'csrf', signal()),
    ).rejects.toThrow()
  await expect(saveInsightsReport(input, '', signal())).rejects.toThrow()
  for (const limit of [0, 101, 1.5])
    await expect(getInsightsReports(signal(), '', limit)).rejects.toThrow()
  await expect(getInsightsReports(signal(), 'x'.repeat(161))).rejects.toThrow()
  await expect(deleteInsightsReport(id.toUpperCase(), 'csrf', signal())).rejects.toThrow()
  expect(calls).toBe(0)
})
it('accepts exact replay and deletion statuses but never normalizes mismatched or canceled save replies', async () => {
  let data: unknown = definition(),
    status = 200,
    calls = 0
  client.defaults.adapter = async (config) => {
    calls++
    return response(config, data, status)
  }
  await expect(saveInsightsReport(input, 'csrf', signal())).resolves.toEqual(definition())
  data = { ...definition(), to: '2026-10-01T00:00:00.123456791Z' }
  await expect(saveInsightsReport(input, 'csrf', signal())).rejects.toThrow()
  data = definition()
  status = 202
  await expect(saveInsightsReport(input, 'csrf', signal())).rejects.toThrow()
  status = 204
  data = ''
  await expect(deleteInsightsReport(id, 'csrf', signal())).resolves.toBeUndefined()
  status = 200
  await expect(deleteInsightsReport(id, 'csrf', signal())).rejects.toThrow()
  const controller = new AbortController()
  controller.abort()
  const before = calls
  await expect(saveInsightsReport(input, 'csrf', controller.signal)).rejects.toThrow()
  await expect(deleteInsightsReport(id, 'csrf', controller.signal)).rejects.toThrow()
  expect(calls).toBe(before)
})
it.each([409, 410, 404])(
  'propagates an explicit HTTP %i without a retry or invented successful receipt',
  async (status) => {
    let calls = 0
    client.defaults.adapter = async (config) => {
      calls++
      throw new AxiosError('sanitized', '', config, undefined, response(config, {}, status))
    }
    if (status === 404)
      await expect(deleteInsightsReport(id, 'csrf', signal())).rejects.toMatchObject({
        response: { status },
      })
    else
      await expect(saveInsightsReport(input, 'csrf', signal())).rejects.toMatchObject({
        response: { status },
      })
    expect(calls).toBe(1)
  },
)

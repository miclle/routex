import client from './client'
import type { LimitInput, LimitRecord } from '@/types/resource-limits'
export async function getLimits(path: string, signal?: AbortSignal) {
  return (await client.get<LimitRecord>(`${path}/limits`, { signal })).data
}
export async function saveLimits(path: string, etag: string, input: LimitInput, csrf: string) {
  return (
    await client.put<LimitRecord>(`${path}/limits`, input, {
      headers: { 'If-Match': `"${etag}"`, 'X-CSRF-Token': csrf },
    })
  ).data
}

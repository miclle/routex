import axios from 'axios'
import client from './client'
import type { AdminOverview, OperationalAlert, UpdateOperationalAlertInput } from '@/types/overview'

export const adminOverviewKey = ['admin', 'overview'] as const

export class OverviewError extends Error {
  constructor(public readonly status: number) {
    super('Overview request failed')
  }
}

export async function getAdminOverview(signal?: AbortSignal) {
  return (await client.get<AdminOverview>('/admin/overview', { signal })).data
}

export async function updateOperationalAlert(
  id: string,
  input: UpdateOperationalAlertInput,
  csrf: string,
) {
  try {
    return (
      await client.patch<OperationalAlert>(
        `/admin/alerts/${encodeURIComponent(id)}`,
        { state: input.state, etag: input.etag },
        { headers: { 'X-CSRF-Token': csrf, 'If-Match': input.etag } },
      )
    ).data
  } catch (error) {
    throw new OverviewError(axios.isAxiosError(error) ? (error.response?.status ?? 0) : 0)
  }
}

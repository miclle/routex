import axios from 'axios'
import client from './client'
import type {
  SystemInstanceCleanupInput,
  SystemInstanceCleanupResult,
  SystemInstancesPage,
  SystemJobsPage,
} from '@/types/system-status'

export const systemInstancesKey = ['admin', 'system-status', 'instances'] as const
export const systemJobsKey = ['admin', 'system-status', 'jobs'] as const

export class SystemStatusError extends Error {
  constructor(public readonly status: number) {
    super('System status request failed')
  }
}

export async function getSystemInstances(signal?: AbortSignal) {
  return (await client.get<SystemInstancesPage>('/admin/system/instances', { signal })).data
}

export async function getSystemJobs(signal?: AbortSignal) {
  return (await client.get<SystemJobsPage>('/admin/system/jobs', { signal })).data
}

export async function cleanupSystemInstances(
  input: SystemInstanceCleanupInput,
  csrf: string,
): Promise<SystemInstanceCleanupResult> {
  try {
    return (
      await client.post<SystemInstanceCleanupResult>('/admin/system/instances/cleanup', input, {
        headers: { 'X-CSRF-Token': csrf },
      })
    ).data
  } catch (error) {
    throw new SystemStatusError(axios.isAxiosError(error) ? (error.response?.status ?? 0) : 0)
  }
}

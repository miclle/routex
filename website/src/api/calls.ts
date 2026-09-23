import client from './client'
import type { AdminCallDetail, CallFilters, CallPage, CallRecord } from '@/types/calls'

export async function listCalls(
  admin: boolean,
  filters: CallFilters,
  cursor: string | null,
  signal?: AbortSignal,
  projectId?: string,
): Promise<CallPage> {
  return (
    await client.get<CallPage>(
      projectId
        ? `/projects/${encodeURIComponent(projectId)}/calls`
        : admin
          ? '/admin/calls'
          : '/calls',
      {
        params: { ...filters, cursor: cursor ?? undefined, limit: 40 },
        signal,
      },
    )
  ).data
}
export async function getCall(
  admin: boolean,
  requestId: string,
  signal?: AbortSignal,
  projectId?: string,
): Promise<CallRecord | AdminCallDetail> {
  return (
    await client.get<CallRecord | AdminCallDetail>(
      `${projectId ? `/projects/${encodeURIComponent(projectId)}/calls` : admin ? '/admin/calls' : '/calls'}/${encodeURIComponent(requestId)}`,
      { signal },
    )
  ).data
}

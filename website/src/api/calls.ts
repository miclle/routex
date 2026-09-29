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

export async function exportCalls(
  admin: boolean,
  filters: CallFilters,
  signal?: AbortSignal,
  projectId?: string,
): Promise<Blob> {
  return (
    await client.get<Blob>(
      projectId
        ? `/projects/${encodeURIComponent(projectId)}/calls/export.csv`
        : admin
          ? '/admin/calls/export.csv'
          : '/calls/export.csv',
      { params: filters, responseType: 'blob', signal },
    )
  ).data
}

export function downloadCallsCSV(blob: Blob, admin: boolean, projectId?: string) {
  const url = URL.createObjectURL(blob)
  const anchor = document.createElement('a')
  anchor.href = url
  anchor.download = projectId
    ? 'routex-project-calls.csv'
    : admin
      ? 'routex-platform-calls.csv'
      : 'routex-personal-calls.csv'
  document.body.append(anchor)
  anchor.click()
  anchor.remove()
  setTimeout(() => URL.revokeObjectURL(url), 0)
}

import axios from 'axios'
import client from './client'
import type {
  CreateProjectRequest,
  ProjectRequest,
  ProjectRequestCandidate,
  ProjectRequestDecision,
  ProjectRequestPage,
  ProjectRequestStatus,
} from '@/types/project-requests'
const projectPath = (id: string) => `/projects/${encodeURIComponent(id)}`
export async function listProjectRequests(
  id: string,
  status: ProjectRequestStatus | '',
  cursor: string | null,
  signal?: AbortSignal,
) {
  return (
    await client.get<ProjectRequestPage>(`${projectPath(id)}/requests`, {
      params: { status: status || undefined, cursor: cursor || undefined, limit: 40 },
      signal,
    })
  ).data
}
export async function projectRequestCandidates(id: string, q: string, signal?: AbortSignal) {
  return (
    await client.get<{ items: ProjectRequestCandidate[] }>(
      `${projectPath(id)}/request-model-candidates`,
      { params: { q }, signal },
    )
  ).data.items
}
export async function createProjectRequest(id: string, input: CreateProjectRequest, csrf: string) {
  return (
    await client.post<ProjectRequest>(`${projectPath(id)}/requests`, input, {
      headers: { 'X-CSRF-Token': csrf },
    })
  ).data
}
export async function decideProjectRequest(
  id: string,
  requestId: string,
  input: ProjectRequestDecision,
  csrf: string,
) {
  return (
    await client.post<ProjectRequest>(
      `${projectPath(id)}/requests/${encodeURIComponent(requestId)}/decision`,
      input,
      { headers: { 'X-CSRF-Token': csrf } },
    )
  ).data
}
export function projectRequestError(error: unknown) {
  const status = axios.isAxiosError(error) ? error.response?.status : undefined
  return status === 409
    ? 'conflict'
    : status === 403 || status === 401
      ? 'denied'
      : status === 400
        ? 'invalid'
        : status === 503
          ? 'unavailable'
          : 'failed'
}

import client from './client'
import { writeCatalog } from './catalog'
import type { CreateProjectKey, ProjectKeyDelivery, ProjectKeyPage } from '@/types/project-keys'
export const projectKeyPath = (projectID: string, keyID?: string) =>
  `/projects/${encodeURIComponent(projectID)}/keys${keyID ? `/${encodeURIComponent(keyID)}` : ''}`
export async function listProjectKeys(
  projectID: string,
  status: string,
  cursor: string | null,
  signal?: AbortSignal,
) {
  return (
    await client.get<ProjectKeyPage>(projectKeyPath(projectID), {
      params: { status: status || undefined, cursor: cursor || undefined, limit: 40 },
      signal,
    })
  ).data
}
export function issueProjectKey(
  projectID: string,
  input: CreateProjectKey | { key_id: string },
  csrf: string,
) {
  return 'key_id' in input
    ? writeCatalog<ProjectKeyDelivery>(
        'post',
        `${projectKeyPath(projectID, input.key_id)}/rotate`,
        { delivery_mode: 'manual' },
        csrf,
      )
    : writeCatalog<ProjectKeyDelivery>('post', projectKeyPath(projectID), input, csrf)
}
export function changeProjectKey(
  projectID: string,
  keyID: string,
  method: 'patch' | 'delete' | 'post',
  csrf: string,
  data?: unknown,
  suffix = '',
) {
  return writeCatalog(method, `${projectKeyPath(projectID, keyID)}${suffix}`, data, csrf)
}

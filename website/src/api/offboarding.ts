import axios from 'axios'
import client from './client'
import type {
  EmergencyOffboarding,
  OffboardingCase,
  OffboardingInventory,
  OffboardingPlan,
} from '@/types/offboarding'
export class OffboardingError extends Error {
  constructor(readonly status: number) {
    super('Offboarding request failed')
  }
}
export async function getOffboarding(userId: string, signal?: AbortSignal) {
  return (
    await client.get<OffboardingInventory>(
      `/admin/members/${encodeURIComponent(userId)}/offboarding`,
      { signal },
    )
  ).data
}
async function post(
  userId: string,
  path: string,
  data: unknown,
  csrf: string,
  reauthenticate = false,
) {
  try {
    const response = await client.post<OffboardingCase>(
      `/admin/members/${encodeURIComponent(userId)}/offboarding/${path}`,
      data,
      {
        headers: { 'X-CSRF-Token': csrf },
        // A wrong reauthentication password is not proof the console session expired.
        validateStatus: (status) =>
          (status >= 200 && status < 300) || (reauthenticate && status === 401),
      },
    )
    if (response.status === 401) throw new OffboardingError(401)
    return response.data
  } catch (error) {
    // Do not retain Axios config, which may contain a reauthentication proof.
    throw new OffboardingError(
      error instanceof OffboardingError
        ? error.status
        : axios.isAxiosError(error)
          ? (error.response?.status ?? 0)
          : 0,
    )
  }
}
export function createOffboardingPlan(userId: string, data: OffboardingPlan, csrf: string) {
  return post(userId, 'plans', data, csrf)
}
export function completeOffboarding(userId: string, caseId: string, csrf: string) {
  return post(userId, `${encodeURIComponent(caseId)}/complete`, {}, csrf)
}
export function emergencyOffboarding(
  userId: string,
  data: EmergencyOffboarding,
  password: string,
  csrf: string,
) {
  return post(userId, 'emergency', { ...data, current_password: password }, csrf, true)
}

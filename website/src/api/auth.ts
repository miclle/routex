import { t } from '@/i18n'
import axios from 'axios'
import client from './client'
import type { LoginInput, Session, SetupInput } from '@/types/auth'

export async function getSetup() {
  return (await client.get<{ initialized: boolean }>('/setup')).data
}

export async function getSession(): Promise<Session | null> {
  try {
    return (await client.get<Session>('/auth/session')).data
  } catch (error) {
    if (axios.isAxiosError(error) && error.response?.status === 401) return null
    throw error
  }
}

export async function setup(input: SetupInput) {
  return (await client.post<Session>('/setup', input)).data
}

export async function login(input: LoginInput) {
  return (await client.post<Session>('/auth/login', input)).data
}

export async function logout(csrfToken: string) {
  try {
    await client.post('/auth/logout', undefined, { headers: { 'X-CSRF-Token': csrfToken } })
  } catch (error) {
    if (axios.isAxiosError(error) && error.response?.status === 401) return
    throw error
  }
}

export function authError(error: unknown): string {
  if (!axios.isAxiosError(error)) return t('the_request_failed_try_again_later_81390')
  switch (error.response?.status) {
    case 401:
      return t('the_email_or_password_is_incorrect_try_again_59a4d')
    case 403:
      return t('this_action_is_not_authorized_refresh_and_retry_31fa7')
    case 409:
      return t('this_site_is_already_set_up_sign_in_337e8')
    case 429:
      return t('too_many_attempts_try_again_later_0a703')
    case 400:
      return t('the_submitted_information_is_invalid_check_it_and_86ba8')
    default:
      return t('unable_to_connect_to_the_service_try_again_0cdbc')
  }
}

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
  if (!axios.isAxiosError(error)) return '请求失败，请稍后重试。'
  switch (error.response?.status) {
    case 401: return '邮箱或密码不正确，请重新输入。'
    case 403: return '操作未获授权，请刷新页面后重试。'
    case 409: return '站点已完成初始化，请使用已有账户登录。'
    case 429: return '尝试次数过多，请稍后再试。'
    case 400: return '提交的信息不符合要求，请检查后重试。'
    default: return '暂时无法连接服务，请稍后重试。'
  }
}

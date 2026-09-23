import axios from 'axios'
import client from './client'
import type { CallableModel, KeyDelivery, Model, PersonalKey, Provider } from '@/types/catalog'

export async function listProviders() { return (await client.get<{ items: Provider[] }>('/admin/providers')).data.items }
export async function listAdminModels() { return (await client.get<{ items: Model[] }>('/admin/models')).data.items }
export async function listModels() { return (await client.get<{ items: CallableModel[] }>('/models')).data.items }
export async function listGrantees() { return (await client.get<{ items: { id: string; email: string; name: string }[] }>('/admin/model-grantees')).data.items }
export async function listKeys() { return (await client.get<{ items: PersonalKey[] }>('/keys')).data.items }
export async function writeCatalog<T = unknown>(method: 'post' | 'put' | 'patch' | 'delete', path: string, data: unknown, csrfToken: string): Promise<T> {
  return (await client.request<T>({ method, url: path, data, headers: { 'X-CSRF-Token': csrfToken } })).data
}
export async function createKey(input: { name: string; model_ids: string[]; expires_at: string | null }, csrf: string) { return writeCatalog<KeyDelivery>('post', '/keys', input, csrf) }
export async function rotateKey(id: string, csrf: string) { return writeCatalog<KeyDelivery>('post', `/keys/${id}/rotate`, {}, csrf) }
export function catalogError(error: unknown) {
  if (axios.isAxiosError(error)) {
    switch (error.response?.status) {
      case 400: return '请检查填写的信息、模型范围和权重设置。'
      case 401: return '登录已过期，请重新登录。'
      case 403: return '没有执行此操作的权限，或模型授权已发生变化。'
      case 404: return '资源不存在或已不可访问，请刷新列表。'
      case 409: return '操作存在冲突：名称可能已使用、状态已变化，或交付已过期。请刷新后重试。'
      case 422: return '配置未通过验证，请检查接入、凭证和模型。'
    }
  }
  return '操作失败，请检查服务连接后重试。'
}

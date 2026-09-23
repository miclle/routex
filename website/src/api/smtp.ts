import axios from 'axios'
import client from './client'
import type {
  SMTPInput,
  SMTPSenderInput,
  SMTPSettings,
  SMTPTest,
  SMTPTestInput,
} from '@/types/smtp'
export class SMTPError extends Error {
  constructor(public readonly status: number) {
    super('SMTP request failed')
  }
}
export async function getSMTP(signal?: AbortSignal) {
  return (await client.get<SMTPSettings>('/admin/smtp', { signal })).data
}
// Do not retain Axios configurations containing authentication or recipient inputs.
async function write<T>(
  method: 'put' | 'post',
  path: string,
  data: unknown,
  csrf: string,
): Promise<T> {
  try {
    return (await client.request<T>({ method, url: path, data, headers: { 'X-CSRF-Token': csrf } }))
      .data
  } catch (error) {
    throw new SMTPError(axios.isAxiosError(error) ? (error.response?.status ?? 0) : 0)
  }
}
export const saveSMTP = (input: SMTPInput, csrf: string) =>
  write<SMTPSettings>('put', '/admin/smtp', input, csrf)
export const saveSMTPSender = (input: SMTPSenderInput, csrf: string) =>
  write<SMTPSettings>('put', '/admin/smtp/sender', input, csrf)
export const sendSMTPTest = (input: SMTPTestInput, csrf: string) =>
  write<SMTPTest>('post', '/admin/smtp/test', input, csrf)

export function validMailbox(value: string) {
  return (
    value.length <= 254 && /^[\x21-\x7e]+$/.test(value) && /^[^\s<>,;@]+@[^\s<>,;@]+$/.test(value)
  )
}

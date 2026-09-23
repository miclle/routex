import client from './client'

// hello fetches the greeting message from the API.
export function hello() {
  return client.get<{ message: string }>('/hello')
}

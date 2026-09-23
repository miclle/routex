import axios from 'axios'

const client = axios.create({
  baseURL: '/api/v1',
  withCredentials: true,
})

// Guards own navigation. Authentication failures must still reach their forms.
client.interceptors.response.use(
  (response) => response,
  (error: unknown) => {
    if (
      axios.isAxiosError(error) &&
      error.response?.status === 401 &&
      !['/auth/login', '/auth/session', '/setup'].includes(error.config?.url ?? '')
    ) {
      window.dispatchEvent(new Event('routex:session-expired'))
    }
    return Promise.reject(error)
  },
)

export default client

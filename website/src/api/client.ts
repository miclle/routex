import axios from 'axios'

// client is the pre-configured axios instance for API calls.
const client = axios.create({
  baseURL: '/api/v1',
  withCredentials: true,
})

// Intercept 401 responses to redirect to login page.
client.interceptors.response.use(
  (response) => response,
  (error) => {
    if (error.response?.status === 401) {
      const path = window.location.pathname
      if (path !== '/login') {
        window.location.href = '/login'
      }
    }
    return Promise.reject(error)
  },
)

export default client

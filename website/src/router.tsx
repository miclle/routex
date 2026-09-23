import type { RouteObject } from 'react-router'
import AuthGate from '@/components/app/AuthGate'
import AppShell from '@/components/app/AppShell'

const routes: RouteObject[] = [
  {
    element: <AuthGate mode="setup" />,
    children: [{ path: '/setup', lazy: async () => { const { default: Page } = await import('@/views/auth'); return { Component: () => <Page mode="setup" /> } } }],
  },
  {
    element: <AuthGate mode="login" />,
    children: [{ path: '/login', lazy: async () => { const { default: Page } = await import('@/views/auth'); return { Component: () => <Page mode="login" /> } } }],
  },
  {
    element: <AuthGate mode="private" />,
    children: [{ path: '/', element: <AppShell />, children: [{ index: true, lazy: async () => ({ Component: (await import('@/views/home')).default }) }] }],
  },
  { path: '*', lazy: async () => ({ Component: (await import('@/views/errors/NotFound')).default }) },
]

export default routes

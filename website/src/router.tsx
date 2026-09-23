import LoadingRoute from '@/components/app/LoadingRoute'
import type { RouteObject } from 'react-router'
import AuthGate from '@/components/app/AuthGate'
import AppShell from '@/components/app/AppShell'

const routes: RouteObject[] = [
  {
    element: <AuthGate mode="setup" />,
    children: [
      {
        path: '/setup',
        lazy: async () => {
          const { default: Page } = await import('@/views/auth')
          return { Component: () => <Page mode="setup" /> }
        },
      },
    ],
  },
  {
    element: <AuthGate mode="login" />,
    children: [
      {
        path: '/register',
        lazy: async () => {
          const { default: Page } = await import('@/views/auth')
          return { Component: () => <Page mode="register" /> }
        },
      },
      {
        path: '/login',
        lazy: async () => {
          const { default: Page } = await import('@/views/auth')
          return { Component: () => <Page mode="login" /> }
        },
      },
    ],
  },
  {
    element: <AuthGate mode="private" />,
    children: [
      {
        path: '/',
        element: <AppShell />,
        children: [
          {
            path: 'admin/system-info',
            lazy: async () => ({ Component: (await import('@/views/site')).default }),
          },
          {
            path: 'admin/system-announcements',
            lazy: async () => ({ Component: (await import('@/views/announcements')).default }),
          },
          {
            path: 'admin/prices',
            lazy: async () => {
              const { default: Page } = await import('@/views/price-imports')
              return { Component: Page }
            },
          },
          {
            path: 'admin/currency',
            lazy: async () => {
              const { default: Page } = await import('@/views/pricing/currency')
              return { Component: Page }
            },
          },
          {
            path: 'admin/providers/:providerId/models/:modelId',
            lazy: async () => {
              const { default: Page } = await import('@/views/pricing/provider-model')
              return { Component: Page }
            },
          },
          {
            path: 'teams',
            lazy: async () => {
              const { default: Page } = await import('@/views/resources/list')
              return { Component: () => <Page kind="teams" /> }
            },
          },
          {
            path: 'teams/:resourceId',
            lazy: async () => {
              const { default: Page } = await import('@/views/resources/detail')
              return { Component: () => <Page kind="teams" /> }
            },
          },
          {
            path: 'admin/teams',
            lazy: async () => {
              const { default: Page } = await import('@/views/resources/list')
              return { Component: () => <Page kind="teams" admin /> }
            },
          },
          {
            path: 'admin/teams/new',
            lazy: async () => {
              const { default: Page } = await import('@/views/resources/create')
              return { Component: () => <Page kind="teams" admin /> }
            },
          },
          {
            path: 'admin/teams/:resourceId',
            lazy: async () => {
              const { default: Page } = await import('@/views/resources/detail')
              return { Component: () => <Page kind="teams" admin /> }
            },
          },
          {
            path: 'projects',
            lazy: async () => {
              const { default: Page } = await import('@/views/resources/list')
              return { Component: () => <Page kind="projects" /> }
            },
          },
          {
            path: 'projects/new',
            lazy: async () => {
              const { default: Page } = await import('@/views/resources/create')
              return { Component: () => <Page kind="projects" /> }
            },
          },
          {
            path: 'projects/:resourceId',
            lazy: async () => {
              const { default: Page } = await import('@/views/resources/detail')
              return { Component: () => <Page kind="projects" /> }
            },
          },
          {
            path: 'admin/projects',
            lazy: async () => {
              const { default: Page } = await import('@/views/resources/list')
              return { Component: () => <Page kind="projects" admin /> }
            },
          },
          {
            path: 'admin/projects/new',
            lazy: async () => {
              const { default: Page } = await import('@/views/resources/create')
              return { Component: () => <Page kind="projects" admin /> }
            },
          },
          {
            path: 'admin/projects/:resourceId',
            lazy: async () => {
              const { default: Page } = await import('@/views/resources/detail')
              return { Component: () => <Page kind="projects" admin /> }
            },
          },
          {
            index: true,
            lazy: async () => ({ Component: (await import('@/views/home')).default }),
          },
          {
            path: 'admin/members',
            lazy: async () => ({ Component: (await import('@/views/governance/members')).default }),
          },
          {
            path: 'admin/members/:memberId/offboarding',
            lazy: async () => ({ Component: (await import('@/views/offboarding')).default }),
          },
          {
            path: 'admin/members/:memberId',
            lazy: async () => ({ Component: (await import('@/views/governance/members')).default }),
          },
          {
            path: 'admin/roles',
            lazy: async () => ({ Component: (await import('@/views/governance/roles')).default }),
          },
          {
            path: 'admin/auth',
            lazy: async () => ({
              Component: (await import('@/views/governance/registration')).default,
            }),
          },
          {
            path: 'account',
            lazy: async () => ({ Component: (await import('@/views/account')).default }),
          },
          {
            path: 'playground',
            lazy: async () => ({ Component: (await import('@/views/playground')).default }),
          },
          {
            path: 'usage',
            lazy: async () => ({ Component: (await import('@/views/usage')).default }),
          },
          {
            path: 'admin/usage',
            lazy: async () => {
              const { default: Page } = await import('@/views/usage')
              return { Component: () => <Page admin /> }
            },
          },
          {
            path: 'calls',
            lazy: async () => ({ Component: (await import('@/views/calls')).default }),
          },
          {
            path: 'admin/calls',
            lazy: async () => {
              const { default: Page } = await import('@/views/calls')
              return { Component: () => <Page admin /> }
            },
          },
          {
            path: 'keys',
            lazy: async () => ({ Component: (await import('@/views/keys')).default }),
          },
          {
            path: 'models',
            lazy: async () => ({ Component: (await import('@/views/models')).default }),
          },
          {
            path: 'admin/providers/:providerId',
            lazy: async () => ({ Component: (await import('@/views/providers')).default }),
          },
          {
            path: 'account/security',
            lazy: async () => {
              const { default: Page } = await import('@/views/account')
              return { Component: () => <Page security /> }
            },
          },
          {
            path: 'admin/providers',
            lazy: async () => ({ Component: (await import('@/views/providers')).default }),
          },
          {
            path: 'admin/models/new',
            lazy: async () => ({ Component: (await import('@/views/models/create')).default }),
          },
          {
            path: 'admin/models/:modelId',
            lazy: async () => ({ Component: (await import('@/views/models/admin')).default }),
          },
          {
            path: 'admin/models',
            lazy: async () => ({ Component: (await import('@/views/models/admin')).default }),
          },
        ],
      },
    ],
  },
  {
    path: '*',
    lazy: async () => ({ Component: (await import('@/views/errors/NotFound')).default }),
  },
]

export default routes.map((route) => ({ ...route, hydrateFallbackElement: <LoadingRoute /> }))

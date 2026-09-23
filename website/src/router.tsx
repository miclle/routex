import type { RouteObject } from 'react-router'

import AppShell from '@/components/app/AppShell'
import Home from 'src/views/home'
import NotFound from 'src/views/errors/NotFound'

const routes: RouteObject[] = [
  {
    path: '/',
    element: <AppShell />,
    children: [
      { index: true, element: <Home /> },
    ],
  },
  { path: '*', element: <NotFound /> },
]

export default routes

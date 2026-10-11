import { ReauthenticationProvider } from '@/views/auth-enforcement/reauthentication-provider'
import { SitePresentation } from '@/components/app/SiteBranding'
import { useSite } from '@/hooks/use-site'
import { createBrowserRouter } from 'react-router'
import { RouterProvider } from 'react-router/dom'
import { AppContext } from 'src/context/app'
import routes from './router'

const router = createBrowserRouter(routes)

// App is the root component wrapping providers and the router.
function App() {
  const site = useSite()
  return (
    <AppContext.Provider value={{ appName: site.data?.name || 'RouteX' }}>
      <SitePresentation />
      <ReauthenticationProvider router={router}>
        <RouterProvider router={router} />
      </ReauthenticationProvider>
    </AppContext.Provider>
  )
}

export default App

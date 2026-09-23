import { NavLink } from 'react-router'
import { Button } from '@/components/ui/button'

// NotFound is the 404 error page.
function NotFound() {
  return (
    <div className="flex items-center justify-center min-h-screen">
      <div className="text-center space-y-4">
        <h1 className="text-6xl font-bold text-foreground">404</h1>
        <p className="text-muted-foreground">Page not found</p>
        <NavLink to="/">
          <Button variant="outline">Back to Home</Button>
        </NavLink>
      </div>
    </div>
  )
}

export default NotFound

import { Server, Sparkles } from 'lucide-react'
import { NavLink, Outlet } from 'react-router'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'

function AppShell() {
  return (
    <div className="min-h-screen bg-background text-foreground">
      <header className="sticky top-0 z-50 border-b bg-background/95 backdrop-blur supports-[backdrop-filter]:bg-background/75">
        <div className="mx-auto flex h-14 max-w-6xl items-center justify-between px-4 sm:px-6">
          <NavLink to="/" className="flex items-center gap-2 text-sm font-semibold no-underline">
            <span className="flex size-8 items-center justify-center rounded-md bg-primary text-primary-foreground">
              <Server className="size-4" aria-hidden="true" />
            </span>
            RouteX
          </NavLink>
          <nav className="flex items-center gap-2">
            <NavLink to="/">
              {({ isActive }) => (
                <Button variant={isActive ? 'secondary' : 'ghost'} size="sm">
                  Home
                </Button>
              )}
            </NavLink>
            <Badge variant="outline" className="hidden gap-1 sm:inline-flex">
              <Sparkles className="size-3" aria-hidden="true" />
              shadcn + Base UI
            </Badge>
          </nav>
        </div>
      </header>

      <main>
        <Outlet />
      </main>
    </div>
  )
}

export default AppShell

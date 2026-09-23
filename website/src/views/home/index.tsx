import { useEffect, useState } from 'react'
import { ArrowRight, CheckCircle2, Code2, Database, PackageCheck } from 'lucide-react'

import { hello } from 'src/api/hello'
import { Badge } from '@/components/ui/badge'
import { buttonVariants } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'

const stackItems = [
  {
    icon: Code2,
    title: 'React SPA',
    description: 'React Router, React Query, Tailwind CSS, and local UI primitives.',
  },
  {
    icon: Database,
    title: 'Go API',
    description: 'Handlers stay behind services and entities, with GORM persistence.',
  },
  {
    icon: PackageCheck,
    title: 'Single Binary',
    description: 'Vite output is embedded into the Go executable for production.',
  },
]

// Home is the landing page that demonstrates the API connection.
function Home() {
  const [message, setMessage] = useState('')
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    hello()
      .then((res) => setMessage(res.data.message))
      .catch(() => setMessage('Failed to connect to API'))
      .finally(() => setLoading(false))
  }, [])

  return (
    <div className="mx-auto flex min-h-[calc(100vh-56px)] max-w-6xl flex-col gap-10 px-4 py-10 sm:px-6 lg:py-16">
      <section className="grid gap-8 lg:grid-cols-[1.1fr_0.9fr] lg:items-center">
        <div className="space-y-6">
          <Badge variant="success" className="gap-1.5">
            <CheckCircle2 className="size-3.5" aria-hidden="true" />
            RouteX development scaffold
          </Badge>
          <div className="space-y-4">
            <h1 className="max-w-3xl text-4xl font-semibold tracking-normal text-balance sm:text-5xl">
              RouteX AI gateway and control plane.
            </h1>
            <p className="max-w-2xl text-base leading-7 text-muted-foreground">
              The Go API and React workspace are ready for development.
              Provider routing, access control, and usage management are planned;
              this page verifies the application scaffold.
            </p>
          </div>
          <div className="flex flex-wrap gap-3">
            <a className={buttonVariants()} href="https://github.com/miclle/routex#development">
              Development guide
              <ArrowRight className="size-4" aria-hidden="true" />
            </a>
            <a className={buttonVariants({ variant: 'outline' })} href="https://github.com/miclle/routex/blob/main/docs/ARCHITECTURE.md">
              Architecture
            </a>
          </div>
        </div>

        <Card className="overflow-hidden">
          <CardHeader>
            <CardTitle>Runtime check</CardTitle>
            <CardDescription>The SPA is reading from the embedded API contract.</CardDescription>
          </CardHeader>
          <CardContent>
            <div className="rounded-md border bg-muted/40 p-4 font-mono text-sm">
              <span className="text-muted-foreground">$ GET /api/v1/hello</span>
              <div className="mt-3 text-foreground">
                {loading ? 'Connecting to API...' : message}
              </div>
            </div>
          </CardContent>
        </Card>
      </section>

      <section>
        <Tabs defaultValue="structure">
          <TabsList aria-label="UI organization">
            <TabsTrigger value="structure">Structure</TabsTrigger>
            <TabsTrigger value="stack">Stack</TabsTrigger>
          </TabsList>
          <TabsContent value="structure">
            <div className="grid gap-4 md:grid-cols-3">
              {stackItems.map((item) => (
                <Card key={item.title}>
                  <CardHeader>
                    <div className="mb-2 flex size-9 items-center justify-center rounded-md bg-secondary">
                      <item.icon className="size-4" aria-hidden="true" />
                    </div>
                    <CardTitle>{item.title}</CardTitle>
                    <CardDescription>{item.description}</CardDescription>
                  </CardHeader>
                </Card>
              ))}
            </div>
          </TabsContent>
          <TabsContent value="stack">
            <Card>
              <CardContent className="pt-5">
                <div className="grid gap-3 text-sm text-muted-foreground sm:grid-cols-2">
                  <p>
                    <span className="font-medium text-foreground">shadcn/ui:</span> copied
                    local components, Tailwind tokens, and CVA variants.
                  </p>
                  <p>
                    <span className="font-medium text-foreground">Base UI:</span> accessible
                    headless behavior wrapped once in local primitives.
                  </p>
                </div>
              </CardContent>
            </Card>
          </TabsContent>
        </Tabs>
      </section>
    </div>
  )
}

export default Home

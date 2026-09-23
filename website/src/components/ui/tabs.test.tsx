import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'

import { Tabs, TabsContent, TabsList, TabsTrigger } from './tabs'

describe('Tabs', () => {
  it('wraps Base UI tabs behind local styled primitives', () => {
    const markup = renderToStaticMarkup(
      <Tabs defaultValue="backend">
        <TabsList aria-label="Stack">
          <TabsTrigger value="backend">Backend</TabsTrigger>
        </TabsList>
        <TabsContent value="backend">Go API</TabsContent>
      </Tabs>,
    )

    expect(markup).toContain('Backend')
    expect(markup).toContain('Go API')
    expect(markup).toContain('data-orientation')
  })
})

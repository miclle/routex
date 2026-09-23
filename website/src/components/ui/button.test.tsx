import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'

import { Button } from './button'

describe('Button', () => {
  it('renders a shadcn-style button with variant classes', () => {
    const markup = renderToStaticMarkup(<Button variant="secondary">Deploy</Button>)

    expect(markup).toContain('Deploy')
    expect(markup).toContain('bg-secondary')
    expect(markup).toContain('rounded-md')
  })
})

import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider, QueryObserver } from '@tanstack/react-query'
import { afterEach, beforeEach, expect, it } from 'vitest'
import { useApprovalCacheRevision } from './approval-authority'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const key = ['permissions', 'usr_admin'] as const
let cache: QueryClient, host: HTMLDivElement, root: Root

function Probe() {
  const boundary = useApprovalCacheRevision([key])
  return <output>{boundary.revision}</output>
}

beforeEach(() => {
  cache = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  cache.setQueryData(key, ['registration.write'])
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
})

afterEach(async () => {
  await act(async () => root.unmount())
  cache.clear()
  host.remove()
})

it.each([
  'added',
  'observerAdded',
  'observerRemoved',
  'observerOptionsUpdated',
  'observerResultsUpdated',
] as const)(
  'ignores render bookkeeping %s without masking real cache transitions',
  async (type) => {
    const observer = new QueryObserver(cache, { queryKey: key, enabled: false })
    const query = cache.getQueryCache().find({ queryKey: key, exact: true })!
    await act(async () =>
      root.render(
        <QueryClientProvider client={cache}>
          <Probe />
        </QueryClientProvider>,
      ),
    )
    const committed = host.textContent
    expect(committed).toBe('success:idle:false:1:0')

    // Stage a changed store fingerprint without its state event. This makes an
    // unwanted notification observable instead of relying on equal-snapshot
    // React bailout. Bookkeeping must not publish it; a real update must.
    query.state = { ...query.state, dataUpdateCount: 2 }
    await act(async () => {
      if (type === 'added') cache.getQueryCache().notify({ type, query })
      else cache.getQueryCache().notify({ type, query, observer })
    })
    expect(cache.getQueryState(key)?.dataUpdateCount).toBe(2)
    expect(host.textContent).toBe(committed)

    await act(async () => {
      cache.setQueryData(key, ['registration.write'])
    })
    expect(host.textContent).toBe('success:idle:false:3:0')

    await act(async () => {
      query.invalidate()
    })
    expect(host.textContent).toBe('success:idle:true:3:0')

    await act(async () => {
      cache.removeQueries({ queryKey: key, exact: true })
    })
    expect(host.textContent).toBe('undefined:undefined:undefined:undefined:undefined')
  },
)

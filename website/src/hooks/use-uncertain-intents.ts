import { createContext, useContext, useSyncExternalStore } from 'react'
import type { SubmittedIntentOwner } from '@/types/uncertain-intents'

type IntentStore = {
  owner: SubmittedIntentOwner
  subscribe: (listener: () => void) => () => void
  snapshot: () => number
}
export const UncertainIntentContext = createContext<IntentStore | null>(null)

// Optional for isolated pages; production private routes supply the boundary.
// The hook subscribes to transient state, never mounts a Session query.
export function useUncertainIntents(): SubmittedIntentOwner | null {
  const store = useContext(UncertainIntentContext)
  useSyncExternalStore(
    store?.subscribe ?? (() => () => undefined),
    store?.snapshot ?? (() => 0),
    () => 0,
  )
  return store?.owner ?? null
}

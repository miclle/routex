import { createContext, useContext } from 'react'

// AppContextValue holds the global application state.
export interface AppContextValue {
  appName: string
}

// AppContext provides access to the global application state.
export const AppContext = createContext<AppContextValue>({
  appName: 'App',
})

// useAppContext returns the current AppContext value.
export function useAppContext() {
  return useContext(AppContext)
}

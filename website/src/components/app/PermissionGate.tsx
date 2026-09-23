import type { ReactNode } from 'react'
import { usePermissions } from '@/hooks/use-permissions'
import { Page, QueryState } from './CatalogUI'
export function PermissionGate({ permission, children }: { permission: string; children: ReactNode }) {
  const access = usePermissions()
  if (access.isPending || access.isError) return <QueryState pending={access.isPending} error={access.error} retry={() => void access.refetch()} />
  return access.can(permission) ? children : <Page title="无权访问" description="当前账户没有访问此页面的权限。"><p role="alert" className="text-sm text-muted-foreground">当前账户没有访问此页面的权限。</p></Page>
}

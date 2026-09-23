import { t } from '@/i18n'
import { useTranslation } from 'react-i18next'
import type { ReactNode } from 'react'
import { usePermissions } from '@/hooks/use-permissions'
import { Page, QueryState } from './CatalogUI'
export function PermissionGate({
  permission,
  children,
}: {
  permission: string
  children: ReactNode
}) {
  useTranslation()

  const access = usePermissions()
  if (access.isPending || access.isError)
    return (
      <QueryState
        pending={access.isPending}
        error={access.error}
        retry={() => void access.refetch()}
      />
    )
  return access.can(permission) ? (
    children
  ) : (
    <Page
      title={t('access_denied_cb8d4')}
      description={t('your_account_does_not_have_permission_to_access_ca6a8')}
    >
      <p role="alert" className="text-sm text-muted-foreground">
        {t('your_account_does_not_have_permission_to_access_ca6a8')}
      </p>
    </Page>
  )
}

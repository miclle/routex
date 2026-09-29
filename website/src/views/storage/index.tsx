import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Cloud } from 'lucide-react'
import { getStorage } from '@/api/storage'
import type { StorageSettings } from '@/types/storage'
import { PermissionGate } from '@/components/app/PermissionGate'
import { Page, QueryState } from '@/components/app/CatalogUI'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { StorageConfiguration } from './editor'

const storageKey = ['admin', 'storage'] as const

export default function StoragePage() {
  return (
    <PermissionGate permission="storage.read">
      <StorageSettingsView />
    </PermissionGate>
  )
}

function StorageSettingsView() {
  const { t } = useTranslation('storage')
  const cache = useQueryClient()
  const query = useQuery({
    queryKey: storageKey,
    queryFn: ({ signal }) => getStorage(signal),
  })
  const [open, setOpen] = useState(false)
  const [notice, setNotice] = useState('')

  async function refresh() {
    const current = await query.refetch()
    if (current.isError || !current.data) {
      throw current.error ?? new Error('Storage refresh failed')
    }
    return current.data
  }

  return (
    <Page title={t('title')} description={t('description')}>
      <div>
        <h1 className="font-semibold">{t('title')}</h1>
        <p className="mt-1 text-sm text-muted-foreground">{t('description')}</p>
      </div>
      <QueryState
        pending={query.isPending}
        error={query.error}
        retry={() => void query.refetch()}
      />
      {notice && (
        <p role="status" className="text-sm">
          {t(notice)}
        </p>
      )}
      {query.data && (
        <StorageOverview
          settings={query.data}
          onConfigure={() => {
            setNotice('')
            setOpen(true)
          }}
        />
      )}
      {open && query.data && (
        <StorageConfiguration
          initial={query.data}
          refresh={refresh}
          onClose={() => setOpen(false)}
          onSaved={async (next, message, close) => {
            cache.setQueryData(storageKey, next)
            await refresh()
            setNotice(message)
            if (close) setOpen(false)
          }}
        />
      )}
    </Page>
  )
}

function StorageOverview({
  settings,
  onConfigure,
}: {
  settings: StorageSettings
  onConfigure: () => void
}) {
  const { t } = useTranslation('storage')
  const revision = settings.revision
  const details = [
    [t('endpoint'), revision?.endpoint || t('notSet')],
    [t('region'), revision?.region || t('notSet')],
    [t('bucket'), revision?.bucket || t('notSet')],
    [t('prefix'), revision?.prefix || t('notSet')],
    [t('credentials'), t(revision?.credentials_configured ? 'configured' : 'notConfigured')],
  ]
  return (
    <section aria-label={t('overview')} className="space-y-4 rounded-lg border p-4">
      <div className="flex items-center justify-between gap-6">
        <div className="flex items-center gap-4">
          <Cloud className="size-6" aria-hidden="true" />
          <div>
            <div className="flex items-center gap-2">
              <h2 className="font-semibold">{t('providerName')}</h2>
              <Badge variant={settings.enabled ? 'success' : 'outline'}>
                {t(settings.enabled ? 'enabled' : 'disabled')}
              </Badge>
            </div>
            <p className="mt-2 text-sm text-muted-foreground">{t('providerDescription')}</p>
          </div>
        </div>
        <Button variant="outline" aria-label={t('configureTitle')} onClick={onConfigure}>
          {t('configure')}
        </Button>
      </div>
      <dl className="grid gap-4 text-sm sm:grid-cols-3">
        <div className="flex flex-wrap gap-x-4 gap-y-1">
          <dt className="text-muted-foreground">{t('provider')}</dt>
          <dd>{t('providerName')}</dd>
        </div>
        {details.map(([label, value]) => (
          <div key={label} className="flex flex-wrap gap-x-4 gap-y-1">
            <dt className="text-muted-foreground">{label}</dt>
            <dd className="break-all">{value}</dd>
          </div>
        ))}
      </dl>
    </section>
  )
}

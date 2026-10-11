import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { getPermissions } from '@/api/governance'
import { getVaultDeliveryOptions, KeyDeliveryOptionsError } from '@/api/key-delivery-options'
import KeyDeliveryAdminWorkspace from '@/views/key-delivery-admin'
import { Button } from '@/components/ui/button'
import { Page } from '@/components/app/CatalogUI'

export function KeyDeliveryWorkspace({
  actor,
  admin,
  fresh,
  generation,
}: {
  actor: string
  admin: boolean
  fresh: boolean
  generation: number
}) {
  const { t } = useTranslation('keyDelivery')
  const permissionKey = ['permissions', actor, 'key-delivery', generation] as const
  const permissions = useQuery({
    queryKey: permissionKey,
    queryFn: ({ signal }) => getPermissions(signal),
    enabled: fresh && admin,
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnMount: 'always',
  })
  const canRead =
    fresh &&
    admin &&
    permissions.isSuccess &&
    !permissions.isFetching &&
    !permissions.error &&
    permissions.data.includes('secrets.read')
  const options = useQuery({
    queryKey: ['key-delivery-vault-options', actor, generation],
    queryFn: ({ signal }) => getVaultDeliveryOptions(signal),
    enabled: canRead,
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnMount: 'always',
  })
  const current = canRead && options.isSuccess && !options.isFetching && !options.error
  return (
    <Page title={t('title')} description={t('scope')}>
      {canRead && options.isError && (
        <div className="space-y-3" role="alert">
          <p>
            {t(
              options.error instanceof KeyDeliveryOptionsError && options.error.kind === 'overflow'
                ? 'optionsOverflow'
                : options.error instanceof KeyDeliveryOptionsError &&
                    options.error.kind === 'changed'
                  ? 'optionsChanged'
                  : 'optionsUnavailable',
            )}
          </p>
          <Button
            variant="outline"
            disabled={options.isFetching}
            onClick={() => void options.refetch()}
          >
            {t('refresh')}
          </Button>
        </div>
      )}
      <KeyDeliveryAdminWorkspace
        actor={actor}
        admin={admin}
        fresh={fresh}
        generation={generation}
        permissionKey={permissionKey}
        vaultChoices={current ? options.data : []}
        choicesCurrent={current}
        choicesGeneration={options.dataUpdatedAt}
      />
    </Page>
  )
}

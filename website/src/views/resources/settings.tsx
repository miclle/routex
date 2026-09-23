import { useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { writeCatalog } from '@/api/catalog'
import { useSession } from '@/hooks/use-auth'
import { ErrorNotice, FormField, SaveButton } from '@/components/app/CatalogUI'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { Dialog } from '@/components/ui/dialog'
import { ResourceSection } from './shared'
import { ResourcePeople } from './people'
import type { ResourceKind, ResourceRecord, ResourceStatus } from '@/types/resources'

export function ResourceSettings({
  resource,
  kind,
  canEdit,
  canLifecycle,
}: {
  resource: ResourceRecord
  kind: ResourceKind
  canEdit: boolean
  canLifecycle: boolean
}) {
  const { t } = useTranslation('resources')
  const session = useSession()
  const cache = useQueryClient()
  const [status, setStatus] = useState<ResourceStatus | null>(null)
  const [saved, setSaved] = useState(false)
  const [invalid, setInvalid] = useState(false)
  const [dirty, setDirty] = useState(false)
  const label = t(kind === 'teams' ? 'team' : 'project')
  const archived = resource.status === 'archived'
  const update = useMutation({
    mutationFn: (data: { name?: string; description?: string; status?: ResourceStatus }) =>
      writeCatalog<ResourceRecord>(
        'patch',
        `${kind === 'teams' ? '/admin/teams' : '/projects'}/${resource.id}`,
        data,
        session.data!.csrf_token,
      ),
    onSuccess: () => {
      setStatus(null)
      setSaved(true)
      setDirty(false)
      void cache.invalidateQueries({ queryKey: ['resources'] })
    },
  })
  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!canEdit || archived || update.isPending) return
    const form = new FormData(event.currentTarget)
    const name = String(form.get('name') ?? '').trim()
    if (!name) {
      setInvalid(true)
      return
    }
    setInvalid(false)
    setSaved(false)
    update.mutate({
      name,
      description: String(form.get('description') ?? '').trim(),
      ...(kind === 'teams' && canLifecycle
        ? { status: String(form.get('status')) as ResourceStatus }
        : {}),
    })
  }
  return (
    <div className="space-y-6">
      {archived && (
        <p role="status" className="text-sm text-muted-foreground">
          {t('archivedReadOnly')}
        </p>
      )}
      <ResourceSection title={t('basic')}>
        <form
          aria-label={t('resourceSettings', { kind: label })}
          onSubmit={submit}
          className="space-y-5"
          onChange={() => {
            setDirty(true)
            setSaved(false)
          }}
        >
          <fieldset disabled={!canEdit || archived || update.isPending} className="space-y-5">
            <FormField label={t('name')}>
              <Input name="name" defaultValue={resource.name} required maxLength={100} />
            </FormField>
            {kind === 'teams' && (
              <FormField label={t('status')}>
                <select
                  name="status"
                  defaultValue={resource.status}
                  className="h-11 w-full rounded-md border bg-background px-3 text-sm"
                  disabled={!canLifecycle || archived}
                >
                  <option value="active">{t('active')}</option>
                  <option value="disabled">{t('disabled')}</option>
                  {archived && <option value="archived">{t('archived')}</option>}
                </select>
              </FormField>
            )}
            <FormField label={kind === 'teams' ? t('description') : t('businessDescription')}>
              <Textarea
                name="description"
                defaultValue={resource.description}
                rows={3}
                maxLength={2000}
              />
            </FormField>
          </fieldset>
          {invalid && (
            <p role="alert" className="text-sm text-destructive">
              {t('blankName')}
            </p>
          )}
          {!status && <ErrorNotice error={update.error} />}
          {canEdit && !archived ? (
            <SaveButton pending={update.isPending} disabled={!dirty}>
              {kind === 'teams' ? t('save') : t('saveSettings')}
            </SaveButton>
          ) : (
            !archived && <p className="text-sm text-muted-foreground">{t('readOnly')}</p>
          )}
          {saved && (
            <p role="status" className="text-sm">
              {t('saved')}
            </p>
          )}
        </form>
      </ResourceSection>
      {kind === 'projects' && (
        <ResourcePeople resource={resource} kind={kind} canEdit={canEdit && !archived} />
      )}
      {kind === 'projects' && canLifecycle && (
        <ResourceSection title={t('lifecycle', { kind: label })}>
          <div className="flex flex-wrap items-center justify-between gap-4">
            <div className="space-y-1">
              <p className="text-sm">
                {t('status')}: {t(resource.status)}
              </p>
              <p className="text-sm text-muted-foreground">{t('lifecycleHelp')}</p>
            </div>
            <div className="flex gap-3">
              {!archived && (
                <Button
                  variant="outline"
                  disabled={update.isPending}
                  onClick={() => {
                    update.reset()
                    setStatus(resource.status === 'active' ? 'disabled' : 'active')
                  }}
                >
                  {resource.status === 'active'
                    ? t('disable', { kind: label })
                    : t('enable', { kind: label })}
                </Button>
              )}
              {resource.status === 'disabled' && (
                <Button
                  variant="outline"
                  className="text-destructive"
                  disabled={update.isPending}
                  onClick={() => {
                    update.reset()
                    setStatus('archived')
                  }}
                >
                  {t('archive', { kind: label })}
                </Button>
              )}
            </div>
          </div>
        </ResourceSection>
      )}
      <Dialog
        open={status !== null}
        onOpenChange={(open) => {
          if (!open) setStatus(null)
        }}
        busy={update.isPending}
        title={t('confirmStatus')}
        description={
          status === 'archived'
            ? t('archiveHelp')
            : t('confirmHelp', { name: resource.name, status: status ? t(status) : '' })
        }
      >
        <form
          className="space-y-4"
          onSubmit={(event) => {
            event.preventDefault()
            if (status && !update.isPending) update.mutate({ status })
          }}
        >
          <ErrorNotice error={update.error} />
          <SaveButton pending={update.isPending}>{t('confirm')}</SaveButton>
        </form>
      </Dialog>
    </div>
  )
}

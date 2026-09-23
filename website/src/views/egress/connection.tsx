import { useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { listProviders } from '@/api/catalog'
import { EgressError, egressSelection, listEgress, saveConnectionEgress } from '@/api/egress'
import type { Connection } from '@/types/catalog'
import { useSession } from '@/hooks/use-auth'
import { usePermissions } from '@/hooks/use-permissions'
import { FormField, QueryState } from '@/components/app/CatalogUI'
import { Button } from '@/components/ui/button'
import { Dialog } from '@/components/ui/dialog'
import { ErrorMessage, selectClass } from './editor'
const valueFor = (connection: Connection) =>
  connection.egress_mode === 'proxy'
    ? `proxy:${connection.egress_id}`
    : (connection.egress_mode ?? 'default')
export function EgressSelect({
  value,
  onChange,
  disabled = false,
}: {
  value?: string
  onChange?: (value: string) => void
  disabled?: boolean
}) {
  const { t } = useTranslation('egress'),
    items = useQuery({
      queryKey: ['egress-options'],
      queryFn: ({ signal }) => listEgress(signal, true),
    })
  return (
    <div className="space-y-2">
      <FormField label={t('selection')}>
        <select
          name="egress_selection"
          className={selectClass}
          value={value}
          defaultValue={value === undefined ? 'default' : undefined}
          disabled={disabled || items.isPending || items.isError}
          onChange={(event) => onChange?.(event.target.value)}
        >
          <option value="default">{t('inherited')}</option>
          <option value="direct">{t('direct')}</option>
          {items.data?.map((item) => (
            <option key={item.id} value={`proxy:${item.id}`} disabled={!item.enabled}>
              {item.name}
              {!item.enabled ? ` · ${t('disabled')}` : ''}
            </option>
          ))}
        </select>
      </FormField>
      <QueryState
        pending={items.isPending}
        error={items.error}
        retry={() => void items.refetch()}
      />
    </div>
  )
}
export function ConnectionEgressControl({ connection }: { connection: Connection }) {
  const { t } = useTranslation('egress'),
    access = usePermissions(),
    [open, setOpen] = useState(false)
  const label =
    connection.egress_mode === 'proxy'
      ? connection.egress_id
      : t(connection.egress_mode === 'direct' ? 'direct' : 'inherited')
  return (
    <>
      <Button
        variant="ghost"
        size="sm"
        disabled={!access.can('providers.write') || !connection.etag}
        onClick={() => setOpen(true)}
      >
        {label}
      </Button>
      {open && <ConnectionEditor initial={connection} onClose={() => setOpen(false)} />}
    </>
  )
}
function ConnectionEditor({ initial, onClose }: { initial: Connection; onClose: () => void }) {
  const { t } = useTranslation('egress'),
    session = useSession(),
    access = usePermissions(),
    cache = useQueryClient(),
    [row, setRow] = useState(initial),
    [value, setValue] = useState(valueFor(initial)),
    [busy, setBusy] = useState(false),
    [error, setError] = useState<number | null>(null),
    lock = useRef(false)
  async function run(reload = false) {
    if (lock.current || !session.data || (!reload && !access.can('providers.write'))) return
    lock.current = true
    setBusy(true)
    setError(null)
    try {
      if (reload) {
        const next = (await listProviders())
          .flatMap((item) => item.connections)
          .find((item) => item.id === initial.id)
        if (!next) throw new EgressError(404)
        setRow(next)
      } else {
        const choice = egressSelection(value)
        await saveConnectionEgress(
          initial.id,
          { etag: row.etag!, mode: choice.egress_mode, egress_id: choice.egress_id },
          session.data.csrf_token,
        )
        await cache.invalidateQueries({ queryKey: ['admin', 'providers'] })
        onClose()
      }
    } catch (failure) {
      setError(failure instanceof EgressError ? failure.status : 0)
    } finally {
      lock.current = false
      setBusy(false)
    }
  }
  return (
    <Dialog
      open
      title={t('connectionTitle')}
      description={t('connectionDescription')}
      busy={busy}
      onOpenChange={(open) => {
        if (!open) onClose()
      }}
    >
      <form
        className="space-y-4"
        onSubmit={(event) => {
          event.preventDefault()
          if (error !== 409 && error !== 503) void run()
        }}
      >
        <p className="text-sm">
          {row.name} · <code>{row.base_url}</code>
        </p>
        <p className="text-sm">
          {t('current')}:{' '}
          {row.egress_mode === 'proxy'
            ? row.egress_id
            : t(row.egress_mode === 'direct' ? 'direct' : 'inherited')}
        </p>
        <EgressSelect value={value} onChange={setValue} disabled={busy} />
        <ErrorMessage status={error} />
        {(error === 409 || error === 503) && (
          <Button type="button" variant="outline" disabled={busy} onClick={() => void run(true)}>
            {t('reload')}
          </Button>
        )}
        <div className="flex justify-end gap-2">
          <Button type="button" variant="outline" disabled={busy} onClick={onClose}>
            {t('cancel')}
          </Button>
          <Button
            type="submit"
            disabled={
              busy || !access.can('providers.write') || !row.etag || error === 409 || error === 503
            }
          >
            {t(busy ? 'saving' : 'save')}
          </Button>
        </div>
      </form>
    </Dialog>
  )
}

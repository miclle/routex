import { useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { MoreHorizontal, Plus } from 'lucide-react'
import {
  EgressError,
  getEgressDefault,
  listEgress,
  saveEgressDefault,
  testEgress,
} from '@/api/egress'
import type { Egress, EgressDefault, EgressDiagnostic } from '@/types/egress'
import { useSession } from '@/hooks/use-auth'
import { usePermissions } from '@/hooks/use-permissions'
import { PermissionGate } from '@/components/app/PermissionGate'
import { FormField, Page, QueryState } from '@/components/app/CatalogUI'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Dialog } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Menu, MenuItem } from '@/components/ui/menu'
import { Table } from '@/components/ui/table'
import EgressEditor, { ErrorMessage, selectClass } from './editor'
import { Diagnostic } from './diagnostic'
export default function EgressPage() {
  return (
    <PermissionGate permission="egress.read">
      <EgressList />
    </PermissionGate>
  )
}
function EgressList() {
  const { t, i18n } = useTranslation('egress'),
    access = usePermissions(),
    cache = useQueryClient()
  const query = useQuery({
      queryKey: ['admin', 'egresses'],
      queryFn: ({ signal }) => listEgress(signal),
    }),
    defaults = useQuery({
      queryKey: ['admin', 'egress-default'],
      queryFn: ({ signal }) => getEgressDefault(signal),
    })
  const [editor, setEditor] = useState<Egress | null | undefined>(),
    [diagnostic, setDiagnostic] = useState<Egress | null>(null),
    [setting, setSetting] = useState<EgressDefault | null>(null),
    [saved, setSaved] = useState(false)
  function refresh() {
    void cache.invalidateQueries({ queryKey: ['admin', 'egresses'] })
    void cache.invalidateQueries({ queryKey: ['admin', 'egress-default'] })
    void cache.invalidateQueries({ queryKey: ['admin', 'providers'] })
    void cache.invalidateQueries({ queryKey: ['egress-options'] })
  }
  return (
    <Page title={t('title')} description={t('description')}>
      <div className="flex flex-wrap items-center justify-between gap-4">
        <div className="flex flex-wrap items-center gap-3">
          <Badge variant="secondary">
            {query.isSuccess
              ? t('measured', {
                  count: query.data.filter((item) => item.last_diagnostic?.transport_ok).length,
                })
              : '—'}
          </Badge>
          {defaults.data && (
            <Button variant="outline" onClick={() => setSetting(defaults.data)}>
              {t('default')}:{' '}
              {defaults.data.egress_id
                ? (query.data?.find((item) => item.id === defaults.data?.egress_id)?.name ??
                  defaults.data.egress_id)
                : t('direct')}
            </Button>
          )}
        </div>
        {access.can('egress.write') && (
          <Button
            onClick={() => {
              setSaved(false)
              setEditor(null)
            }}
          >
            <Plus className="size-4" />
            {t('add')}
          </Button>
        )}
      </div>
      {saved && <p role="status">{t('saved')}</p>}
      <QueryState
        pending={query.isPending || defaults.isPending}
        error={query.error || defaults.error}
        retry={() => {
          void query.refetch()
          void defaults.refetch()
        }}
        empty={query.isSuccess && !query.data.length}
      />
      {query.data && (
        <Table aria-label={t('title')} className="min-w-[900px]">
          <thead>
            <tr>
              <th>{t('title')}</th>
              <th>{t('state')}</th>
              <th>{t('protocol')}</th>
              <th>{t('latency')}</th>
              <th>{t('providers')}</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {query.data.map((item) => (
              <tr key={item.id}>
                <td>
                  <p>{item.name}</p>
                  <code className="text-xs text-muted-foreground">
                    {item.host}:{item.port}
                  </code>
                </td>
                <td>
                  <Badge variant="outline">{t(item.enabled ? 'enabled' : 'disabled')}</Badge>
                </td>
                <td>{item.kind.toUpperCase()}</td>
                <td>
                  {item.last_diagnostic
                    ? `${item.last_diagnostic.duration_ms} ms`
                    : t('notChecked')}
                  {item.last_checked_at && (
                    <p className="text-xs text-muted-foreground">
                      {t('lastCheck', {
                        date: new Date(item.last_checked_at).toLocaleString(i18n.resolvedLanguage),
                      })}
                    </p>
                  )}
                </td>
                <td>
                  {item.providers.map((provider) => provider.name).join(', ') || t('unlinked')}
                </td>
                <td>
                  <Menu
                    trigger={<MoreHorizontal className="size-4" />}
                    label={t('actions', { name: item.name })}
                  >
                    <MenuItem
                      disabled={!access.can('egress.test')}
                      onClick={() => setDiagnostic(item)}
                    >
                      {t('test')}
                    </MenuItem>
                    <MenuItem
                      disabled={!access.can('egress.write')}
                      onClick={() => {
                        setSaved(false)
                        setEditor(item)
                      }}
                    >
                      {t('edit')}
                    </MenuItem>
                  </Menu>
                </td>
              </tr>
            ))}
          </tbody>
        </Table>
      )}
      {editor !== undefined && (
        <EgressEditor
          initial={editor}
          onClose={() => {
            setEditor(undefined)
            refresh()
          }}
          onSaved={() => {
            setEditor(undefined)
            setSaved(true)
            refresh()
          }}
        />
      )}
      {diagnostic && (
        <DiagnosticDialog
          initial={diagnostic}
          onClose={() => {
            setDiagnostic(null)
            refresh()
          }}
        />
      )}
      {setting && (
        <DefaultDialog
          initial={setting}
          items={query.data ?? []}
          onClose={() => setSetting(null)}
          onSaved={() => {
            setSetting(null)
            setSaved(true)
            refresh()
          }}
        />
      )}
    </Page>
  )
}
function DiagnosticDialog({ initial, onClose }: { initial: Egress; onClose: () => void }) {
  const { t } = useTranslation('egress'),
    session = useSession(),
    access = usePermissions()
  const [row, setRow] = useState(initial),
    [target, setTarget] = useState(''),
    [result, setResult] = useState<EgressDiagnostic | null>(null),
    [busy, setBusy] = useState(false),
    [error, setError] = useState<number | null>(null),
    lock = useRef(false)
  async function run(reload = false) {
    if (lock.current || !session.data || (!reload && !access.can('egress.test'))) return
    lock.current = true
    setBusy(true)
    setError(null)
    try {
      if (reload) {
        const current = (await listEgress()).find((item) => item.id === initial.id)
        if (!current) throw new EgressError(404)
        setRow(current)
        setResult(null)
      } else
        setResult(
          await testEgress(
            row.id,
            { etag: row.etag, target_base_url: target.trim() },
            session.data.csrf_token,
          ),
        )
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
      title={t('diagnostics')}
      description={t('diagnosticHelp')}
      width={720}
      busy={busy}
      onOpenChange={(open) => {
        if (!open) onClose()
      }}
    >
      <form
        className="space-y-4"
        onSubmit={(event) => {
          event.preventDefault()
          void run()
        }}
      >
        <dl className="rounded-md border text-sm">
          <div className="grid grid-cols-2 p-3">
            <dt>{t('address')}</dt>
            <dd>
              <code>
                {row.host}:{row.port}
              </code>
            </dd>
          </div>
        </dl>
        <FormField label={t('target')}>
          <Input
            name="diagnostic_target"
            type="url"
            required
            value={target}
            disabled={busy}
            onValueChange={(value) => {
              setTarget(value)
              setResult(null)
            }}
          />
        </FormField>
        <p className="text-xs text-muted-foreground">{t('targetHelp')}</p>
        <ErrorMessage status={error} />
        {result && <Diagnostic result={result} />}
        <div className="flex justify-end gap-2">
          {(error === 409 || result?.stale) && (
            <Button type="button" variant="outline" disabled={busy} onClick={() => void run(true)}>
              {t('reload')}
            </Button>
          )}
          <Button type="button" variant="outline" disabled={busy} onClick={onClose}>
            {t('close')}
          </Button>
          <Button
            type="submit"
            disabled={
              busy || !target.trim() || error === 409 || result?.stale || !access.can('egress.test')
            }
          >
            {t(busy ? 'testing' : 'test')}
          </Button>
        </div>
      </form>
    </Dialog>
  )
}
function DefaultDialog({
  initial,
  items,
  onClose,
  onSaved,
}: {
  initial: EgressDefault
  items: Egress[]
  onClose: () => void
  onSaved: () => void
}) {
  const { t } = useTranslation('egress'),
    session = useSession(),
    access = usePermissions(),
    [revision, setRevision] = useState(initial),
    [value, setValue] = useState(initial.egress_id ?? ''),
    [busy, setBusy] = useState(false),
    [error, setError] = useState<number | null>(null),
    lock = useRef(false)
  async function run(reload = false) {
    if (lock.current || !session.data || (!reload && !access.can('egress.write'))) return
    lock.current = true
    setBusy(true)
    setError(null)
    try {
      if (reload) setRevision(await getEgressDefault())
      else {
        await saveEgressDefault(
          { egress_id: value || null, etag: revision.etag },
          session.data.csrf_token,
        )
        onSaved()
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
      title={t('setDefault')}
      description={t('defaultHelp')}
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
          {t('current')}:{' '}
          {revision.egress_id
            ? (items.find((item) => item.id === revision.egress_id)?.name ?? revision.egress_id)
            : t('direct')}
        </p>
        <FormField label={t('default')}>
          <select
            name="default_egress"
            className={selectClass}
            value={value}
            disabled={busy || !access.can('egress.write')}
            onChange={(event) => setValue(event.target.value)}
          >
            <option value="">{t('direct')}</option>
            {items.map((item) => (
              <option key={item.id} value={item.id} disabled={!item.enabled}>
                {item.name}
                {!item.enabled ? ` · ${t('disabled')}` : ''}
              </option>
            ))}
          </select>
        </FormField>
        <ErrorMessage status={error} />
        {(error === 409 || error === 503) && (
          <Button type="button" variant="outline" disabled={busy} onClick={() => void run(true)}>
            {t('reload')}
          </Button>
        )}
        <div className="flex justify-end gap-2">
          <Button type="button" variant="outline" disabled={busy} onClick={onClose}>
            {t('close')}
          </Button>
          {access.can('egress.write') && (
            <Button type="submit" disabled={busy || error === 409 || error === 503}>
              {t(busy ? 'saving' : 'save')}
            </Button>
          )}
        </div>
      </form>
    </Dialog>
  )
}

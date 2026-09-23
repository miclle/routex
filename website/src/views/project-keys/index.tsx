import { useEffect, useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { useInfiniteQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Copy, Plus } from 'lucide-react'
import { changeProjectKey, issueProjectKey, listProjectKeys } from '@/api/project-keys'
import { useSession } from '@/hooks/use-auth'
import { usePermissions } from '@/hooks/use-permissions'
import { ErrorNotice, FormField, QueryState, SaveButton } from '@/components/app/CatalogUI'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Dialog } from '@/components/ui/dialog'
import { Drawer } from '@/components/ui/drawer'
import { Input } from '@/components/ui/input'
import { Table } from '@/components/ui/table'
import type { ResourceRecord } from '@/types/resources'
import type { CreateProjectKey, ProjectKey, ProjectKeyDelivery } from '@/types/project-keys'

export default function ProjectKeysPanel({ project }: { project: ResourceRecord }) {
  return <ProjectKeys key={project.id} project={project} />
}
function ProjectKeys({ project }: { project: ResourceRecord }) {
  const { t, i18n } = useTranslation('projectKeys')
  const session = useSession()
  const access = usePermissions()
  const cache = useQueryClient()
  const allowed =
    access.can('projects.write') ||
    !!project.managers?.some((manager) => manager.user_id === session.data?.user.id)
  const active = project.status === 'active'
  const writable = allowed && project.status !== 'archived'
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    if (!allowed) return
    const timer = window.setInterval(() => setNow(Date.now()), 1000)
    return () => window.clearInterval(timer)
  }, [allowed])
  const [filter, setFilter] = useState('')
  const [creating, setCreating] = useState(false)
  const [delivery, setDelivery] = useState<ProjectKeyDelivery | null>(null)
  const [saved, setSaved] = useState(false)
  const [viewing, setViewing] = useState<ProjectKey | null>(null)
  const [action, setAction] = useState<{
    kind: 'rename' | 'revoke' | 'complete'
    key: ProjectKey
  } | null>(null)
  const [notice, setNotice] = useState<string | null>(null)
  const [validation, setValidation] = useState<string | null>(null)
  const keys = useInfiniteQuery({
    queryKey: ['project-keys', project.id, filter],
    queryFn: ({ pageParam, signal }) => listProjectKeys(project.id, filter, pageParam, signal),
    initialPageParam: null as string | null,
    getNextPageParam: (page) => page.next_cursor ?? undefined,
    enabled: allowed,
  })
  const candidates = useInfiniteQuery({
    queryKey: ['project-key-replacements', project.id],
    queryFn: ({ pageParam, signal }) => listProjectKeys(project.id, 'active', pageParam, signal),
    initialPageParam: null as string | null,
    getNextPageParam: (page) => page.next_cursor ?? undefined,
    enabled: allowed && action?.kind === 'complete',
  })
  const refresh = async () => {
    await Promise.all([
      cache.invalidateQueries({ queryKey: ['project-keys', project.id] }),
      cache.invalidateQueries({ queryKey: ['project-key-replacements', project.id] }),
    ])
  }
  const issue = useMutation({
    mutationFn: async (input: CreateProjectKey | { key_id: string }) => {
      // Never return the one-time secret into a React Query mutation cache.
      const result = await issueProjectKey(project.id, input, session.data!.csrf_token)
      setDelivery(result)
      setSaved(false)
      setCreating(false)
      setNotice(null)
    },
    gcTime: 0,
    onSuccess: refresh,
  })
  const update = useMutation({
    mutationFn: ({
      key,
      method,
      data,
      suffix,
    }: {
      key: ProjectKey
      method: 'patch' | 'delete' | 'post'
      data?: unknown
      suffix?: string
    }) => changeProjectKey(project.id, key.id, method, session.data!.csrf_token, data, suffix),
    onSuccess: (_, input) => {
      if (input.suffix === '/complete-rotation') setNotice('completed')
      setAction(null)
      void refresh()
    },
  })
  const finish = useMutation({
    mutationFn: (confirm: boolean) =>
      changeProjectKey(
        project.id,
        delivery!.key.id,
        confirm ? 'post' : 'delete',
        session.data!.csrf_token,
        confirm ? {} : undefined,
        confirm ? '/confirm' : '',
      ),
    onSuccess: (_, confirmed) => {
      setNotice(confirmed ? (delivery?.key.replaces_key_id ? 'rotationNotice' : 'confirmed') : null)
      setDelivery(null)
      setSaved(false)
      void refresh()
    },
  })
  const busy = issue.isPending || update.isPending || finish.isPending
  const expired = (key: ProjectKey) => !!key.expires_at && new Date(key.expires_at).getTime() <= now
  const deliveryExpired = (key: ProjectKey) =>
    key.status === 'pending' &&
    !!key.delivery_expires_at &&
    Date.parse(key.delivery_expires_at) <= now
  const date = (value: string | null) =>
    value
      ? new Date(value).toLocaleString(i18n.resolvedLanguage === 'zh' ? 'zh-CN' : 'en-US')
      : t('never')
  const rows = keys.data?.pages.flatMap((page) => page.items) ?? []
  const replacements =
    candidates.data?.pages
      .flatMap((page) => page.items)
      .filter((key) => key.replaces_key_id === action?.key.id && !expired(key)) ?? []
  function openAction(kind: 'rename' | 'revoke' | 'complete', key: ProjectKey) {
    update.reset()
    setAction({ kind, key })
  }
  function create(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (busy || !active || !writable) return
    const form = new FormData(event.currentTarget)
    const model_ids = form.getAll('model_ids').map(String)
    const expiry = String(form.get('expires_at') || '')
    if (!model_ids.length) {
      setValidation('scopeRequired')
      return
    }
    if (expiry && (!Number.isFinite(Date.parse(expiry)) || Date.parse(expiry) <= Date.now())) {
      setValidation('expiryInvalid')
      return
    }
    setValidation(null)
    issue.mutate({
      name: String(form.get('name')).trim(),
      model_ids,
      expires_at: expiry ? new Date(expiry).toISOString() : null,
      delivery_mode: 'manual',
    })
  }
  async function copy() {
    try {
      await navigator.clipboard.writeText(delivery!.secret)
      setNotice('copied')
    } catch {
      setNotice('copyFailed')
    }
  }
  if (!allowed)
    return (
      <p role="status" className="text-sm text-muted-foreground">
        {session.isPending || access.isPending ? '…' : t('denied')}
      </p>
    )
  return (
    <section className="space-y-5" aria-label={t('title')}>
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div>
          <h2 className="font-semibold">{t('title')}</h2>
          <p className="mt-1 max-w-3xl text-sm text-muted-foreground">{t('description')}</p>
        </div>
        <Button
          disabled={!active || !writable || busy || !project.model_ids.length}
          onClick={() => {
            issue.reset()
            finish.reset()
            setValidation(null)
            setCreating(true)
          }}
        >
          <Plus className="size-4" />
          {t('create')}
        </Button>
      </div>
      {active && !project.model_ids.length && (
        <p role="status" className="text-sm text-muted-foreground">
          {t('noModels')}
        </p>
      )}
      {!active && (
        <p className="rounded-md border bg-muted/30 p-3 text-sm">
          {t(project.status === 'archived' ? 'archived' : 'inactive')}
        </p>
      )}
      {notice && !delivery && (
        <p role="status" className="text-sm">
          {t(notice)}
        </p>
      )}
      <div>
        <select
          aria-label={t('filter')}
          value={filter}
          onChange={(event) => setFilter(event.target.value)}
          className="h-10 rounded-md border bg-background px-3 text-sm"
        >
          {['', 'active', 'disabled', 'pending', 'revoked'].map((status) => (
            <option key={status} value={status}>
              {t(status || 'all')}
            </option>
          ))}
        </select>
      </div>
      <ErrorNotice error={!creating && !delivery && !action ? issue.error || update.error : null} />
      <QueryState
        pending={keys.isPending}
        error={keys.error}
        retry={() => void keys.refetch()}
        empty={keys.isSuccess && rows.length === 0}
      />
      <Table aria-label={t('list')}>
        <thead>
          <tr>
            <th>{t('name')}</th>
            <th>{t('key')}</th>
            <th>{t('delivery')}</th>
            <th>{t('scope')}</th>
            <th>{t('status')}</th>
            <th>{t('expires')}</th>
            <th>{t('actions')}</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((key) => (
            <tr key={key.id}>
              <td>
                {key.name}
                {key.replaces_key_id && (
                  <p className="mt-1 text-xs text-muted-foreground">
                    {t('replacementOf', { id: key.replaces_key_id })}
                  </p>
                )}
              </td>
              <td className="font-mono">{key.prefix}…</td>
              <td>
                <Badge variant="outline">{t('manual')}</Badge>
              </td>
              <td>
                <Button
                  size="sm"
                  variant="ghost"
                  aria-label={`${t('view')} · ${key.name}`}
                  onClick={() => setViewing(key)}
                >
                  {t('count', { count: key.model_ids.length })}
                </Button>
              </td>
              <td>
                <Badge variant="outline">{t(key.status)}</Badge>
                {(expired(key) || deliveryExpired(key)) && (
                  <span className="ml-2 text-xs text-muted-foreground">{t('expired')}</span>
                )}
              </td>
              <td className="whitespace-nowrap">{date(key.expires_at)}</td>
              <td>
                <div className="flex flex-wrap gap-1">
                  {(key.status === 'active' || key.status === 'disabled') && (
                    <>
                      <Button
                        size="sm"
                        variant="ghost"
                        disabled={!writable || busy}
                        onClick={() => openAction('rename', key)}
                      >
                        {t('rename')}
                      </Button>
                      <Button
                        size="sm"
                        variant="ghost"
                        disabled={
                          !writable ||
                          busy ||
                          (key.status === 'disabled' && (!active || expired(key)))
                        }
                        onClick={() =>
                          update.mutate({
                            key,
                            method: 'patch',
                            data: { enabled: key.status !== 'active' },
                          })
                        }
                      >
                        {t(key.status === 'active' ? 'disable' : 'enable')}
                      </Button>
                      <Button
                        size="sm"
                        variant="ghost"
                        disabled={!writable || !active || busy || expired(key)}
                        onClick={() => {
                          issue.reset()
                          finish.reset()
                          issue.mutate({ key_id: key.id })
                        }}
                      >
                        {t('rotate')}
                      </Button>
                      <Button
                        size="sm"
                        variant="ghost"
                        disabled={!writable || !active || busy}
                        onClick={() => openAction('complete', key)}
                      >
                        {t('complete')}
                      </Button>
                    </>
                  )}
                  {key.status !== 'revoked' && (
                    <Button
                      size="sm"
                      variant="ghost"
                      disabled={!writable || busy}
                      onClick={() => openAction('revoke', key)}
                    >
                      {t('revoke')}
                    </Button>
                  )}
                </div>
              </td>
            </tr>
          ))}
        </tbody>
      </Table>
      {keys.hasNextPage && (
        <Button
          variant="outline"
          disabled={keys.isFetchingNextPage}
          onClick={() => void keys.fetchNextPage()}
        >
          {t('more')}
        </Button>
      )}
      <Dialog
        open={creating}
        onOpenChange={setCreating}
        title={t('create')}
        description={t('createDescription')}
        busy={busy}
      >
        <form onSubmit={create} className="space-y-5">
          <fieldset disabled={busy || !active || !writable} className="space-y-5">
            <FormField label={t('name')}>
              <Input name="name" required maxLength={100} />
            </FormField>
            <FormField label={t('expiryOptional')}>
              <Input name="expires_at" type="datetime-local" />
            </FormField>
            <fieldset className="space-y-2">
              <legend className="mb-2 text-sm font-medium">{t('scope')}</legend>
              {!project.model_ids.length && <p>{t('noModels')}</p>}
              {project.model_ids.map((id) => (
                <label key={id} className="flex items-center gap-2 text-sm">
                  <input type="checkbox" name="model_ids" value={id} />
                  <span className="break-all font-mono text-xs">{id}</span>
                </label>
              ))}
            </fieldset>
            <p className="text-sm text-muted-foreground">{t('manual')}</p>
          </fieldset>
          {validation && (
            <p role="alert" className="text-sm text-destructive">
              {t(validation)}
            </p>
          )}
          <ErrorNotice error={issue.error} />
          <SaveButton pending={issue.isPending} disabled={!active || !writable}>
            {t('create')}
          </SaveButton>
        </form>
      </Dialog>
      <Dialog
        open={!!delivery}
        onOpenChange={(open) => {
          if (!open && !busy) {
            if (writable) finish.mutate(false)
            else setDelivery(null)
          }
        }}
        title={t('deliveryTitle')}
        description={t('deliveryDescription')}
        busy={busy}
      >
        <div className="space-y-5">
          {delivery?.key.replaces_key_id && (
            <p className="rounded-md border bg-muted/30 p-3 text-sm">{t('rotationNotice')}</p>
          )}
          <code
            className="block break-all rounded-md border bg-muted p-4 text-sm"
            data-testid="project-key-secret"
          >
            {delivery?.secret}
          </code>
          <Button variant="outline" onClick={() => void copy()}>
            <Copy className="size-4" />
            {t('copy')}
          </Button>
          {notice && (
            <p role="status" className="text-sm">
              {t(notice)}
            </p>
          )}
          {delivery?.key.delivery_expires_at && (
            <p className="text-xs text-muted-foreground">
              {t('deadline', { date: date(delivery.key.delivery_expires_at) })}
            </p>
          )}
          <label className="flex items-center gap-2 text-sm">
            <input
              type="checkbox"
              checked={saved}
              onChange={(event) => setSaved(event.target.checked)}
            />
            {t('savedCheck')}
          </label>
          <ErrorNotice error={finish.error} />
          <div className="flex flex-wrap justify-end gap-3">
            <Button
              variant="outline"
              disabled={busy || !writable}
              onClick={() => finish.mutate(false)}
            >
              {t('discard')}
            </Button>
            <Button
              disabled={
                !saved ||
                busy ||
                !active ||
                !writable ||
                !delivery ||
                expired(delivery.key) ||
                (!!delivery.key.delivery_expires_at &&
                  Date.parse(delivery.key.delivery_expires_at) <= now)
              }
              onClick={() => finish.mutate(true)}
            >
              {t('confirm')}
            </Button>
          </div>
        </div>
      </Dialog>
      <Dialog
        open={!!action}
        onOpenChange={(open) => {
          if (!open) setAction(null)
        }}
        title={t(
          action?.kind === 'revoke'
            ? 'revokeTitle'
            : action?.kind === 'complete'
              ? 'complete'
              : 'rename',
        )}
        description={t(
          action?.kind === 'revoke'
            ? 'revokeDescription'
            : action?.kind === 'complete'
              ? 'completeDescription'
              : 'renameDescription',
        )}
        busy={busy}
      >
        <form
          onSubmit={(event) => {
            event.preventDefault()
            if (!action || busy || !writable) return
            const form = new FormData(event.currentTarget)
            update.mutate({
              key: action.key,
              method:
                action.kind === 'rename' ? 'patch' : action.kind === 'revoke' ? 'delete' : 'post',
              data:
                action.kind === 'rename'
                  ? { name: String(form.get('name')).trim() }
                  : action.kind === 'complete'
                    ? { replacement_key_id: String(form.get('replacement_key_id')) }
                    : undefined,
              suffix: action.kind === 'complete' ? '/complete-rotation' : '',
            })
          }}
          className="space-y-5"
        >
          {action?.kind === 'rename' && (
            <FormField label={t('name')}>
              <Input name="name" defaultValue={action.key.name} required maxLength={100} />
            </FormField>
          )}
          {action?.kind === 'complete' && (
            <>
              <QueryState
                pending={candidates.isPending}
                error={candidates.error}
                retry={() => void candidates.refetch()}
              />
              <FormField label={t('replacement')}>
                <select
                  name="replacement_key_id"
                  required
                  className="h-10 rounded-md border bg-background px-3"
                >
                  <option value="">{t('selectReplacement')}</option>
                  {replacements.map((key) => (
                    <option key={key.id} value={key.id}>
                      {key.name} · {key.prefix}…
                    </option>
                  ))}
                </select>
              </FormField>
              {candidates.isSuccess && !replacements.length && (
                <p className="text-sm text-muted-foreground">{t('noReplacement')}</p>
              )}
              {candidates.hasNextPage && (
                <Button
                  variant="outline"
                  disabled={candidates.isFetchingNextPage}
                  onClick={() => void candidates.fetchNextPage()}
                >
                  {t('moreReplacements')}
                </Button>
              )}
            </>
          )}
          <ErrorNotice error={update.error} />
          <SaveButton
            pending={update.isPending}
            disabled={
              !writable || (action?.kind === 'complete' && (!active || !replacements.length))
            }
          >
            {t(
              action?.kind === 'revoke'
                ? 'confirmRevoke'
                : action?.kind === 'complete'
                  ? 'complete'
                  : 'save',
            )}
          </SaveButton>
        </form>
      </Dialog>
      <Drawer
        open={!!viewing}
        onOpenChange={(open) => {
          if (!open) setViewing(null)
        }}
        title={viewing?.name ?? t('scope')}
        description={t('immutable')}
        width={520}
      >
        <dl className="space-y-4 text-sm">
          <div>
            <dt className="text-muted-foreground">{t('project')}</dt>
            <dd>{project.name}</dd>
          </div>
          <div>
            <dt className="text-muted-foreground">{t('creator')}</dt>
            <dd className="break-all font-mono text-xs">{viewing?.creator_id}</dd>
          </div>
          <div>
            <dt className="text-muted-foreground">{t('expires')}</dt>
            <dd>{date(viewing?.expires_at ?? null)}</dd>
          </div>
        </dl>
        <h3 className="mt-6 mb-3 font-medium">{t('scope')}</h3>
        <ul className="space-y-3">
          {viewing?.model_ids.map((id) => (
            <li key={id} className="rounded-md border p-3">
              <code className="break-all text-xs">{id}</code>
              {!project.model_ids.includes(id) && (
                <p className="mt-1 text-xs text-muted-foreground">{t('removedGrant')}</p>
              )}
            </li>
          ))}
        </ul>
      </Drawer>
    </section>
  )
}

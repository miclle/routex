import { useRef, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import axios from 'axios'
import { Bell, Pencil, Plus, CircleStop } from 'lucide-react'
import {
  getAnnouncements,
  publishAnnouncement,
  editAnnouncement,
  closeAnnouncement,
} from '@/api/site'
import type { Announcement } from '@/types/site'
import { locale } from '@/i18n'
import { useSession } from '@/hooks/use-auth'
import { usePermissions } from '@/hooks/use-permissions'
import { PermissionGate } from '@/components/app/PermissionGate'
import { Page, QueryState } from '@/components/app/CatalogUI'
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card'
import { Table } from '@/components/ui/table'
import { Button } from '@/components/ui/button'
import { Dialog } from '@/components/ui/dialog'
const announcementsKey = ['announcements'] as const
type Action = { mode: 'create' } | { mode: 'edit' | 'close'; record: Announcement }
function date(value: string) {
  return new Intl.DateTimeFormat(locale(), { dateStyle: 'medium', timeStyle: 'short' }).format(
    new Date(value),
  )
}
export default function AnnouncementsPage() {
  const session = useSession()
  return (
    <PermissionGate permission="system.read">
      <History key={session.data?.user.id} />
    </PermissionGate>
  )
}
function History() {
  const { t } = useTranslation('announcements')
  const session = useSession()
  const access = usePermissions()
  const cache = useQueryClient()
  const [cursors, setCursors] = useState<(string | undefined)[]>([undefined])
  const [action, setAction] = useState<Action | null>(null)
  const [notice, setNotice] = useState('')
  const cursor = cursors.at(-1)
  const query = useQuery({
    queryKey: [...announcementsKey, session.data?.user.id, 'admin', cursor],
    queryFn: ({ signal }) => getAnnouncements(true, cursor, signal),
    retry: false,
  })
  async function completed(mode: Action['mode']) {
    setAction(null)
    setNotice(mode === 'create' ? 'published' : mode === 'edit' ? 'edited' : 'closedNotice')
    if (mode === 'create') setCursors([undefined])
    await cache.invalidateQueries({ queryKey: announcementsKey })
  }
  return (
    <Page title={t('title')} description={t('records')}>
      <Card>
        <CardHeader className="flex-row flex-wrap items-center justify-between gap-3">
          <CardTitle className="flex items-center gap-2">
            <Bell className="size-4" aria-hidden="true" />
            {t('records')}
          </CardTitle>
          {access.can('announcements.write') && (
            <Button
              onClick={() => {
                setNotice('')
                setAction({ mode: 'create' })
              }}
            >
              <Plus className="size-4" aria-hidden="true" />
              {t('publish')}
            </Button>
          )}
        </CardHeader>
        <CardContent className="p-0">
          {notice && (
            <p role="status" className="px-4 py-3 text-sm">
              {t(notice)}
            </p>
          )}
          <div className="px-4">
            <QueryState
              pending={query.isPending}
              error={query.error}
              retry={() => void query.refetch()}
            />
          </div>
          {query.isSuccess && (
            <>
              <Table aria-label={t('records')} className="min-w-[900px]">
                <thead>
                  <tr>
                    {['content', 'status', 'created', 'updated', 'actions'].map((column) => (
                      <th key={column}>{t(column)}</th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {query.data.items.map((record) => (
                    <tr key={record.id}>
                      <td className="min-w-64 max-w-xl whitespace-pre-wrap break-words">
                        {record.content}
                      </td>
                      <td>
                        <span
                          className={`rounded border px-2 py-1 text-xs ${record.status === 'active' ? 'border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:text-emerald-400' : 'text-muted-foreground'}`}
                        >
                          {t(record.status)}
                        </span>
                      </td>
                      <td className="whitespace-nowrap">{date(record.created_at)}</td>
                      <td className="whitespace-nowrap">{date(record.updated_at)}</td>
                      <td>
                        {access.can('announcements.write') && (
                          <div className="flex gap-1">
                            <Button
                              variant="ghost"
                              onClick={() => {
                                setNotice('')
                                setAction({ mode: 'edit', record })
                              }}
                            >
                              <Pencil className="size-3" aria-hidden="true" />
                              {t('edit')}
                            </Button>
                            {record.status === 'active' && (
                              <Button
                                variant="ghost"
                                className="text-destructive"
                                onClick={() => {
                                  setNotice('')
                                  setAction({ mode: 'close', record })
                                }}
                              >
                                <CircleStop className="size-3" aria-hidden="true" />
                                {t('close')}
                              </Button>
                            )}
                          </div>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </Table>
              {query.data.items.length === 0 && (
                <p className="p-8 text-center text-sm text-muted-foreground">{t('empty')}</p>
              )}
              {(cursors.length > 1 || query.data.next_cursor) && (
                <nav className="flex items-center justify-end gap-3 p-4" aria-label={t('records')}>
                  <Button
                    variant="outline"
                    disabled={cursors.length === 1}
                    onClick={() => setCursors(cursors.slice(0, -1))}
                  >
                    {t('previous')}
                  </Button>
                  <span className="text-sm">{t('page', { page: cursors.length })}</span>
                  <Button
                    variant="outline"
                    disabled={!query.data.next_cursor}
                    onClick={() => setCursors([...cursors, query.data.next_cursor!])}
                  >
                    {t('next')}
                  </Button>
                </nav>
              )}
            </>
          )}
        </CardContent>
      </Card>
      {action && access.can('announcements.write') && (
        <ActionDialog
          action={action}
          dismiss={() => setAction(null)}
          done={() => void completed(action.mode)}
          reload={async (id) => {
            const result = await query.refetch()
            return result.isSuccess ? result.data.items.find((item) => item.id === id) : undefined
          }}
        />
      )}
    </Page>
  )
}
function ActionDialog({
  action,
  dismiss,
  done,
  reload,
}: {
  action: Action
  dismiss: () => void
  done: () => void
  reload: (id: string) => Promise<Announcement | undefined>
}) {
  const { t } = useTranslation('announcements')
  const session = useSession()
  const access = usePermissions()
  const [record, setRecord] = useState(action.mode === 'create' ? null : action.record)
  const [content, setContent] = useState(action.mode === 'edit' ? action.record.content : '')
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const [busy, setBusy] = useState(false)
  const lock = useRef(false)
  const isClose = action.mode === 'close'
  const stale = error === 'conflict' || error === 'missing'
  async function submit() {
    if (lock.current || stale || !access.can('announcements.write')) return
    const normalized = content.trim()
    if (
      !isClose &&
      (Array.from(normalized).length < 1 ||
        Array.from(normalized).length > 4000 ||
        normalized.includes('\0'))
    ) {
      setError('invalid')
      return
    }
    lock.current = true
    setBusy(true)
    setError('')
    setNotice('')
    try {
      const csrf = session.data!.csrf_token
      if (action.mode === 'create') await publishAnnouncement(normalized, csrf)
      else if (action.mode === 'close') await closeAnnouncement(record!.id, record!.etag, csrf)
      else await editAnnouncement(record!.id, normalized, record!.etag, csrf)
      done()
    } catch (cause) {
      const status = axios.isAxiosError(cause) ? cause.response?.status : 0
      setError(
        status === 409
          ? 'conflict'
          : status === 403
            ? 'denied'
            : status === 400
              ? 'invalid'
              : 'failed',
      )
    } finally {
      lock.current = false
      setBusy(false)
    }
  }
  async function review() {
    if (lock.current || !record) return
    lock.current = true
    setBusy(true)
    try {
      const next = await reload(record.id)
      if (next) {
        setRecord(next)
        setError('')
        setNotice('reviewed')
      } else setError('missing')
    } finally {
      lock.current = false
      setBusy(false)
    }
  }
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) dismiss()
      }}
      title={t(isClose ? 'closeTitle' : action.mode === 'edit' ? 'editTitle' : 'publish')}
      description={t(isClose ? 'closeHint' : 'editorHint')}
      width={640}
      busy={busy}
    >
      <form
        onSubmit={(event) => {
          event.preventDefault()
          void submit()
        }}
        className="space-y-5"
      >
        {record && (
          <Card>
            <CardHeader>
              <CardTitle>{t('original')}</CardTitle>
            </CardHeader>
            <CardContent>
              <p className="whitespace-pre-wrap break-words text-sm">{record.content}</p>
              {record.status === 'closed' && (
                <p className="mt-3 text-sm text-muted-foreground">{t('closed')}</p>
              )}
            </CardContent>
          </Card>
        )}
        {!isClose && (
          <label className="block space-y-2 text-sm font-medium">
            <span>{t('content')}</span>
            <textarea
              name="content"
              aria-label={t('content')}
              required
              rows={8}
              value={content}
              disabled={busy}
              onChange={(event) => {
                setContent(event.target.value)
                setNotice('')
              }}
              placeholder={t('placeholder')}
              className="w-full resize-y rounded-md border bg-background px-3 py-2 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            />
            <span className="block text-right text-xs font-normal text-muted-foreground">
              {Array.from(content).length.toLocaleString(locale())} / 4,000
            </span>
          </label>
        )}
        {error && (
          <p role="alert" className="text-sm text-destructive">
            {t(error)}
          </p>
        )}
        {stale && (
          <Button variant="outline" disabled={busy} onClick={() => void review()}>
            {t('reload')}
          </Button>
        )}
        {notice && (
          <p role="status" className="text-sm">
            {t(notice)}
          </p>
        )}
        <div className="flex justify-end gap-3">
          <Button variant="outline" disabled={busy} onClick={dismiss}>
            {t('cancel')}
          </Button>
          <Button
            type="submit"
            disabled={busy || stale || !access.can('announcements.write')}
            variant={isClose ? 'outline' : 'default'}
            className={isClose ? 'text-destructive' : ''}
          >
            {t(
              busy
                ? 'working'
                : isClose
                  ? 'confirmClose'
                  : action.mode === 'edit'
                    ? 'save'
                    : 'publishAction',
            )}
          </Button>
        </div>
      </form>
    </Dialog>
  )
}

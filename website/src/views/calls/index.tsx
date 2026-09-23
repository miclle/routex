import { useTranslation } from 'react-i18next'
import { useState, type FormEvent } from 'react'
import { useInfiniteQuery, useQuery } from '@tanstack/react-query'
import { RefreshCw } from 'lucide-react'
import { getCall, listCalls } from '@/api/calls'
import type { AdminCallDetail, CallFilters, CallRecord } from '@/types/calls'
import { Page, QueryState, FormField, ErrorNotice } from '@/components/app/CatalogUI'
import { PermissionGate } from '@/components/app/PermissionGate'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Drawer } from '@/components/ui/drawer'

export default function CallsPage({
  admin = false,
  projectId,
}: {
  admin?: boolean
  projectId?: string
}) {
  return admin ? (
    <PermissionGate permission="calls.read_all">
      <CallRecords admin />
    </PermissionGate>
  ) : (
    <CallRecords admin={false} projectId={projectId} />
  )
}
function CallRecords({ admin, projectId }: { admin: boolean; projectId?: string }) {
  const { t, i18n } = useTranslation('activity')
  const formatTime = (value: string) =>
    new Date(value).toLocaleString(i18n.resolvedLanguage === 'zh' ? 'zh-CN' : 'en-US')
  const scope = projectId ? ['project', projectId] : [admin ? 'admin' : 'self']
  const [filters, setFilters] = useState<CallFilters>({})
  const [selected, setSelected] = useState<string | null>(null)
  const [validation, setValidation] = useState('')
  const calls = useInfiniteQuery({
    queryKey: ['calls', ...scope, filters],
    queryFn: ({ pageParam, signal }) => listCalls(admin, filters, pageParam, signal, projectId),
    initialPageParam: null as string | null,
    getNextPageParam: (page) => page.next_cursor ?? undefined,
  })
  const detail = useQuery({
    queryKey: ['call', ...scope, selected],
    queryFn: ({ signal }) => getCall(admin, selected!, signal, projectId),
    enabled: selected !== null,
  })
  function filter(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const data = new FormData(event.currentTarget)
    const next: CallFilters = {}
    for (const name of ['status', 'model_id', 'key_id', ...(admin ? ['user_id'] : [])] as const) {
      const value = String(data.get(name) ?? '').trim()
      if (value) next[name as keyof CallFilters] = value
    }
    for (const name of ['from', 'to'] as const) {
      const value = String(data.get(name) ?? '')
      if (value) next[name] = new Date(value).toISOString()
    }
    if (next.from && next.to && next.from > next.to) {
      setValidation('calls.invalidRange')
      return
    }
    setValidation('')
    setSelected(null)
    setFilters(next)
  }
  const items = calls.data?.pages.flatMap((page) => page.items) ?? []
  return (
    <Page
      title={projectId ? t('calls.projectCalls') : admin ? t('calls.allCalls') : t('calls.myCalls')}
      description={
        projectId
          ? t('calls.projectDescription')
          : admin
            ? t('calls.adminDescription')
            : t('calls.description')
      }
      action={
        <Button variant="outline" disabled={calls.isFetching} onClick={() => void calls.refetch()}>
          <RefreshCw className="size-4" aria-hidden="true" />
          {t('calls.refresh')}
        </Button>
      }
    >
      <form
        onSubmit={filter}
        aria-label={t('calls.filters')}
        className="flex flex-wrap items-end gap-2 [&_label]:w-44 [&_label]:text-xs [&_input]:h-9 [&_select]:h-9"
      >
        <FormField label={t('calls.status')}>
          <select
            name="status"
            className="h-11 w-full rounded-md border bg-background px-3 text-sm"
          >
            <option value="">{t('calls.allStatuses')}</option>
            <option value="success">{t('calls.success')}</option>
            <option value="error">{t('calls.error')}</option>
            <option value="canceled">{t('calls.canceled')}</option>
          </select>
        </FormField>
        <FormField label={t('calls.modelID')}>
          <Input name="model_id" placeholder={t('calls.optional')} />
        </FormField>
        <FormField label={t('calls.keyID')}>
          <Input name="key_id" placeholder={t('calls.optional')} />
        </FormField>
        {admin && (
          <FormField label={t('calls.userID')}>
            <Input name="user_id" placeholder={t('calls.optional')} />
          </FormField>
        )}
        <FormField label={t('calls.from')}>
          <Input name="from" type="datetime-local" />
        </FormField>
        <FormField label={t('calls.to')}>
          <Input name="to" type="datetime-local" />
        </FormField>
        <div className="flex items-end gap-2">
          <Button type="submit" disabled={calls.isFetching}>
            {t('calls.filter')}
          </Button>
          <Button
            type="reset"
            variant="ghost"
            onClick={() => {
              setFilters({})
              setSelected(null)
              setValidation('')
            }}
          >
            {t('calls.reset')}
          </Button>
        </div>
        {validation && (
          <p role="alert" className="text-sm text-destructive sm:col-span-2">
            {t(validation)}
          </p>
        )}
      </form>
      <QueryState
        pending={calls.isPending}
        error={calls.isFetchNextPageError ? null : calls.error}
        retry={() => void calls.refetch()}
        empty={calls.isSuccess && items.length === 0}
      />
      {items.length > 0 && (
        <div className="overflow-x-auto">
          <table className="w-full text-left text-sm">
            <thead className="border-b bg-muted/40 text-xs text-muted-foreground">
              <tr>
                <th className="px-3 py-2 font-medium">{t('calls.modelRequest')}</th>
                {admin && <th className="px-3 py-2 font-medium">{t('calls.owner')}</th>}
                <th className="px-3 py-2 font-medium">{t('calls.status')}</th>
                <th className="px-3 py-2 font-medium">{t('calls.from')}</th>
                <th className="px-3 py-2 font-medium">{t('calls.duration')}</th>
                <th className="px-3 py-2 font-medium">{t('calls.tokens')}</th>
                <th className="px-3 py-2 font-medium">
                  <span className="sr-only">{t('calls.details')}</span>
                </th>
              </tr>
            </thead>
            <tbody className="divide-y">
              {items.map((call) => (
                <tr key={call.request_id}>
                  <td className="max-w-72 px-3 py-2">
                    <p className="break-all font-medium">{call.model_name}</p>
                    <p className="mt-1 break-all font-mono text-xs text-muted-foreground">
                      {call.request_id}
                    </p>
                  </td>
                  {admin && (
                    <td className="px-3 py-2 font-mono text-xs">
                      {'project_id' in call && call.project_id
                        ? call.project_id
                        : 'user_id' in call
                          ? call.user_id || '—'
                          : '—'}
                    </td>
                  )}
                  <td className="whitespace-nowrap px-3 py-2">
                    <Badge variant="outline">{t(`calls.${call.status}`)}</Badge>
                  </td>
                  <td className="whitespace-nowrap px-3 py-2 text-xs">
                    {formatTime(call.started_at)}
                  </td>
                  <td className="whitespace-nowrap px-3 py-2">{call.duration_ms} ms</td>
                  <td className="whitespace-nowrap px-3 py-2">
                    {call.input_tokens ?? t('calls.missing')} /{' '}
                    {call.output_tokens ?? t('calls.missing')}
                  </td>
                  <td className="px-3 py-2">
                    <Button
                      size="sm"
                      variant="ghost"
                      aria-label={t('calls.viewRequest', { id: call.request_id })}
                      onClick={() => setSelected(call.request_id)}
                    >
                      {t('calls.details')}
                    </Button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {calls.isFetchNextPageError && <ErrorNotice error={calls.error} />}
      {calls.hasNextPage && (
        <div className="text-center">
          <Button
            variant="outline"
            disabled={calls.isFetchingNextPage}
            onClick={() => void calls.fetchNextPage()}
          >
            {calls.isFetchingNextPage
              ? t('calls.loading')
              : calls.isFetchNextPageError
                ? t('calls.retryMore')
                : t('calls.loadMore')}
          </Button>
        </div>
      )}
      <Drawer
        open={selected !== null}
        onOpenChange={(open) => {
          if (!open) setSelected(null)
        }}
        title={t('calls.detailTitle')}
        description={admin ? t('calls.adminDetailDescription') : t('calls.detailDescription')}
      >
        <QueryState
          pending={detail.isPending}
          error={detail.error}
          retry={() => void detail.refetch()}
        />
        {detail.data && <CallDetail call={detail.data} admin={admin} />}
      </Drawer>
    </Page>
  )
}
function CallDetail({ call, admin }: { call: CallRecord | AdminCallDetail; admin: boolean }) {
  const { t, i18n } = useTranslation('activity')
  const formatTime = (value: string) =>
    new Date(value).toLocaleString(i18n.resolvedLanguage === 'zh' ? 'zh-CN' : 'en-US')
  return (
    <div className="space-y-5">
      <dl className="divide-y rounded-md border text-sm [&>div]:grid [&>div]:grid-cols-[140px_1fr] [&>div]:gap-3 [&>div]:p-3">
        {[
          [t('calls.requestID'), call.request_id],
          [t('calls.model'), call.model_name],
          [t('calls.modelID'), call.model_id],
          [t('calls.keyID'), call.key_id],
          [t('calls.protocol'), call.protocol],
          [t('calls.status'), t(`calls.${call.status}`)],
          [t('calls.responseMode'), call.stream ? t('calls.stream') : t('calls.ordinary')],
          [t('calls.duration'), `${call.duration_ms} ms`],
          [t('calls.from'), formatTime(call.started_at)],
          [t('calls.completedAt'), formatTime(call.completed_at)],
          [t('calls.inputTokens'), call.input_tokens ?? t('calls.missing')],
          [t('calls.outputTokens'), call.output_tokens ?? t('calls.missing')],
        ].map(([name, value]) => (
          <div key={name}>
            <dt className="text-xs text-muted-foreground">{name}</dt>
            <dd className="mt-1 break-all">{value}</dd>
          </div>
        ))}
      </dl>
      {admin && 'attempts' in call && (
        <section className="space-y-3 border-t pt-5">
          <h3 className="text-sm font-medium">{t('calls.upstreamDiagnostics')}</h3>
          <dl className="grid grid-cols-2 gap-3 text-xs">
            <div>
              <dt className="text-muted-foreground">
                {call.project_id ? t('calls.projectID') : t('calls.user')}
              </dt>
              <dd className="mt-1 break-all">{call.project_id || call.user_id || '—'}</dd>
            </div>
            <div>
              <dt className="text-muted-foreground">{t('calls.errorCode')}</dt>
              <dd className="mt-1 break-all">{call.error_code || '—'}</dd>
            </div>
            <div>
              <dt className="text-muted-foreground">{t('calls.providerModel')}</dt>
              <dd className="mt-1 break-all">{call.provider_model_id || '—'}</dd>
            </div>
            <div>
              <dt className="text-muted-foreground">{t('calls.connection')}</dt>
              <dd className="mt-1 break-all">{call.connection_id || '—'}</dd>
            </div>
          </dl>
          <h4 className="text-xs font-medium">{t('calls.attempts')}</h4>
          {call.attempts.length === 0 && (
            <p className="text-xs text-muted-foreground">{t('calls.noAttempts')}</p>
          )}
          {call.attempts.map((attempt) => (
            <div key={attempt.id} className="space-y-1 rounded-md border p-3 text-xs">
              <p className="break-all font-mono">{attempt.id}</p>
              <p>
                {t(`calls.${attempt.status}`, { defaultValue: attempt.status })} · HTTP{' '}
                {attempt.http_status || '—'} · {attempt.error_code || t('calls.noErrorCode')}
              </p>
              <p className="break-all text-muted-foreground">
                {attempt.connection_id} / {attempt.provider_model_id}
              </p>
              <p className="text-muted-foreground">
                {formatTime(attempt.started_at)} → {formatTime(attempt.completed_at)}
              </p>
            </div>
          ))}
        </section>
      )}
    </div>
  )
}

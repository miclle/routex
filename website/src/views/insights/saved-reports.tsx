import {
  useCallback,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  useSyncExternalStore,
} from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { isAxiosError } from 'axios'
import { MoreHorizontal } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import {
  deleteInsightsReport,
  getInsightsReports,
  reportParameters,
  saveInsightsReport,
} from '@/api/insights-reports'
import { sessionKey } from '@/hooks/use-auth'
import type { Session } from '@/types/auth'
import type {
  InsightsReportDefinition,
  InsightsReportInput,
  InsightsReportKind,
  InsightsReportParameters,
} from '@/types/insights'
import { Button } from '@/components/ui/button'
import { Card, CardHeader, CardTitle } from '@/components/ui/card'
import { Dialog } from '@/components/ui/dialog'
import { Menu, MenuItem } from '@/components/ui/menu'
import { Table } from '@/components/ui/table'

interface Options {
  actor: string
  authorized: (parameters: InsightsReportParameters) => boolean
  loadAuthorized?: (kind: InsightsReportKind) => boolean
  reviewStamp?: (parameters: InsightsReportParameters) => string | null
  capture?: () => InsightsReportParameters | null
  load?: (parameters: InsightsReportParameters) => void
}
export function useSavedInsightsReports(options: Options) {
  const { actor, authorized, capture, load, loadAuthorized } = options
  const currentOptions = useRef(options)
  useLayoutEffect(() => {
    currentOptions.current = options
  })
  const canLoad = (parameters: InsightsReportParameters) =>
    loadAuthorized ? loadAuthorized(parameters.kind) : authorized(parameters)
  const { t, i18n } = useTranslation('insights')
  const cache = useQueryClient()
  const sessionSnapshot = useCallback(() => {
    const state = cache.getQueryState<Session | null>(sessionKey)
    return JSON.stringify([
      state?.status,
      state?.fetchStatus,
      state?.isInvalidated,
      state?.dataUpdateCount,
      state?.data?.user.id,
    ])
  }, [cache])
  const subscribe = useCallback(
    (changed: () => void) =>
      cache.getQueryCache().subscribe((event) => {
        if (JSON.stringify(event.query.queryKey) === JSON.stringify(sessionKey)) changed()
      }),
    [cache],
  )
  const sessionStamp = useSyncExternalStore(subscribe, sessionSnapshot, sessionSnapshot)
  const [initialStamp] = useState(sessionStamp)
  const sessionState = cache.getQueryState<Session | null>(sessionKey)
  const currentOwner =
    sessionStamp === initialStamp &&
    sessionState?.status === 'success' &&
    sessionState.fetchStatus === 'idle' &&
    !sessionState.isInvalidated &&
    sessionState.data?.user.id === actor
  const alive = useRef(true)
  const owner = useCallback(() => {
    const state = cache.getQueryState<Session | null>(sessionKey)
    return (
      alive.current &&
      sessionSnapshot() === initialStamp &&
      state?.status === 'success' &&
      state.fetchStatus === 'idle' &&
      !state.isInvalidated &&
      state.data?.user.id === actor
    )
  }, [actor, cache, sessionSnapshot, initialStamp])
  const [after, setAfter] = useState('')
  const [history, setHistory] = useState<string[]>([])
  const queryKey = useMemo(
    () => ['insights-saved-reports', actor, initialStamp, after],
    [actor, initialStamp, after],
  )
  const page = useQuery({
    queryKey,
    enabled: currentOwner,
    queryFn: async ({ signal }) => {
      if (!owner()) throw new DOMException('Aborted', 'AbortError')
      const result = await getInsightsReports(signal, after)
      if (!owner() || signal.aborted) throw new DOMException('Aborted', 'AbortError')
      return result
    },
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnMount: 'always',
    refetchOnWindowFocus: true,
  })
  const controllers = useRef(new Set<AbortController>())
  const saveLock = useRef(false)
  const deleteLock = useRef(false)
  const [intent, setIntent] = useState<InsightsReportInput | null>(null)
  const intentRef = useRef<InsightsReportInput | null>(null)
  const [saving, setSaving] = useState(false)
  const [notice, setNotice] = useState('')
  const [selected, setSelected] = useState<InsightsReportDefinition | null>(null)
  const [deleting, setDeleting] = useState(false)
  const [deleteNotice, setDeleteNotice] = useState('')
  useLayoutEffect(() => {
    alive.current = true
    const activeControllers = controllers.current
    const stop = cache.getQueryCache().subscribe(() => {
      if (!owner()) for (const controller of activeControllers) controller.abort()
    })
    return () => {
      alive.current = false
      stop()
      for (const controller of activeControllers) controller.abort()
      intentRef.current = null
      void cache.cancelQueries({ queryKey: ['insights-saved-reports', actor, initialStamp] })
      cache.removeQueries({ queryKey: ['insights-saved-reports', actor, initialStamp] })
    }
  }, [cache, actor, initialStamp, owner])
  const csrf = () => (owner() ? (cache.getQueryData<Session>(sessionKey)?.csrf_token ?? '') : '')
  const refresh = () => {
    if (!owner()) return
    if (after) {
      setAfter('')
      setHistory([])
    } else void page.refetch()
  }
  async function save() {
    if (!owner() || saveLock.current) return
    let input = intentRef.current
    if (!input) {
      const parameters = capture?.()
      if (!parameters || !currentOptions.current.authorized(parameters)) return
      input = { request_id: crypto.randomUUID(), ...parameters }
      intentRef.current = input
      setIntent(input)
    }
    if (!currentOptions.current.authorized(input)) return
    const currentIntent = input
    const reviewedStamp = currentOptions.current.reviewStamp?.(currentIntent)
    if (reviewedStamp === null) return
    const reviewCurrent = () =>
      currentOptions.current.reviewStamp?.(currentIntent) === reviewedStamp
    const controller = new AbortController()
    controllers.current.add(controller)
    saveLock.current = true
    setSaving(true)
    setNotice('')
    try {
      await saveInsightsReport(currentIntent, csrf(), controller.signal)
      if (
        !owner() ||
        !currentOptions.current.authorized(currentIntent) ||
        !reviewCurrent() ||
        controller.signal.aborted ||
        intentRef.current !== currentIntent
      )
        return
      intentRef.current = null
      setIntent(null)
      setNotice('savedConfirmed')
      refresh()
    } catch (error) {
      if (
        !owner() ||
        !currentOptions.current.authorized(currentIntent) ||
        !reviewCurrent() ||
        controller.signal.aborted
      )
        return
      const status = isAxiosError(error) ? error.response?.status : undefined
      setNotice(
        status === 409
          ? 'saveConflict'
          : status === 410
            ? 'saveDeleted'
            : [400, 422].includes(status ?? 0)
              ? 'saveInvalid'
              : [401, 403].includes(status ?? 0)
                ? 'saveDenied'
                : status === 404
                  ? 'saveEndpointUnavailable'
                  : 'saveUnknown',
      )
    } finally {
      controllers.current.delete(controller)
      saveLock.current = false
      if (owner()) setSaving(false)
    }
  }
  async function remove() {
    if (!selected || !owner() || deleteLock.current) return
    const definition = selected
    const controller = new AbortController()
    controllers.current.add(controller)
    deleteLock.current = true
    setDeleting(true)
    setDeleteNotice('')
    try {
      await deleteInsightsReport(definition.id, csrf(), controller.signal)
      if (!owner() || controller.signal.aborted) return
      setSelected(null)
      setNotice('deleteConfirmed')
      refresh()
    } catch (error) {
      if (owner() && !controller.signal.aborted) {
        const status = isAxiosError(error) ? error.response?.status : undefined
        setDeleteNotice(
          status === 404
            ? 'deleteMissing'
            : [401, 403].includes(status ?? 0)
              ? 'deleteDenied'
              : 'deleteUnknown',
        )
      }
    } finally {
      controllers.current.delete(controller)
      deleteLock.current = false
      if (owner()) setDeleting(false)
    }
  }
  const rows =
    currentOwner &&
    page.isSuccess &&
    !page.isFetching &&
    !cache.getQueryState(queryKey)?.isInvalidated
      ? page.data
      : undefined
  const title = (kind: InsightsReportKind) =>
    t(
      kind === 'provider_quality'
        ? 'providerReportTitle'
        : kind === 'team_quota'
          ? 'quotaTeamTitle'
          : kind === 'key_quota'
            ? 'quotaKeyTitle'
            : 'reportTitle',
    )
  const parametersText = (definition: InsightsReportParameters) =>
    definition.kind === 'team_quota' || definition.kind === 'key_quota'
      ? t('savedQuotaParameters', {
          dimension: t(definition.dimension === 'tokens' ? 'quotaTokens' : 'quotaMoney'),
          scope:
            definition.kind === 'team_quota'
              ? t('savedReadableTeams')
              : t('savedKeyScope', {
                  kind: t(
                    definition.scope_kind === 'member' ? 'quotaMemberScope' : 'quotaProjectScope',
                  ),
                  id: definition.scope_id,
                }),
        })
      : t('savedParameters', { timezone: definition.timezone })
  const rangeText = (definition: InsightsReportParameters) =>
    definition.kind === 'team_quota' || definition.kind === 'key_quota'
      ? t('savedCurrentQuotaWindow')
      : t('savedRange', { from: definition.from, to: definition.to })
  const createdAt = (definition: InsightsReportDefinition) =>
    new Date(definition.created_at).toLocaleString(
      i18n.resolvedLanguage === 'zh' ? 'zh-CN' : 'en-US',
      { timeZone: 'timezone' in definition ? definition.timezone : 'UTC', timeZoneName: 'short' },
    )
  const actorName = cache.getQueryData<Session>(sessionKey)?.user.name || actor
  const captured = currentOwner ? capture?.() : null
  const canSave = currentOwner && (intent ? authorized(intent) : !!captured && authorized(captured))
  return {
    saveButton: (
      <Button variant="outline" disabled={!canSave || saving} onClick={() => void save()}>
        {t(saving ? 'savingReport' : intent ? 'retrySave' : 'saveReport')}
      </Button>
    ),
    table: currentOwner ? (
      <>
        <Card>
          <CardHeader>
            <CardTitle>{t('savedTitle')}</CardTitle>
            <p className="text-sm text-muted-foreground">{t('savedPrivacy')}</p>
          </CardHeader>
          {notice && (
            <p role="status" className="px-6 pb-3">
              {t(notice)}
            </p>
          )}
          {intent && (
            <div className="space-y-2 px-6 pb-3 text-sm">
              <p>
                {intent.kind === 'team_quota' || intent.kind === 'key_quota'
                  ? t('saveQuotaIntent', {
                      title: title(intent.kind),
                      parameters: parametersText(intent),
                    })
                  : t('saveIntent', {
                      title: title(intent.kind),
                      from: intent.from,
                      to: intent.to,
                      timezone: intent.timezone,
                    })}
              </p>
              <Button
                variant="outline"
                size="sm"
                disabled={saving}
                onClick={() => {
                  if (saveLock.current) return
                  intentRef.current = null
                  setIntent(null)
                  setNotice('')
                }}
              >
                {t('discardSave')}
              </Button>
            </div>
          )}
          <Table aria-label={t('savedTitle')}>
            <thead>
              <tr>
                {['reportColumn', 'parametersColumn', 'rangeColumn', 'creatorColumn'].map((key) => (
                  <th key={key}>{t(key)}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {rows ? (
                rows.items.length ? (
                  rows.items.map((definition) => (
                    <tr key={definition.id}>
                      <td>
                        <div className="flex items-center justify-between gap-2">
                          <span>
                            {title(definition.kind)}
                            <span className="mt-1 block text-xs text-muted-foreground">
                              {t('savedCreatedAt', { time: createdAt(definition) })}
                            </span>
                          </span>
                          <Menu
                            side="bottom"
                            align="end"
                            trigger={<MoreHorizontal className="size-4" aria-hidden />}
                            label={t('savedActions', {
                              title: title(definition.kind),
                              id: definition.id,
                            })}
                            triggerClassName="size-8 w-8 justify-center"
                            popupClassName="min-w-48"
                          >
                            <MenuItem
                              disabled={!load || !canLoad(definition) || saving}
                              onClick={() => {
                                if (owner() && !saveLock.current && load && canLoad(definition))
                                  load(reportParameters(definition))
                              }}
                            >
                              {t('loadSaved')}
                            </MenuItem>
                            <MenuItem
                              disabled={deleting}
                              onClick={() => {
                                if (owner() && !deleteLock.current) {
                                  setSelected(definition)
                                  setDeleteNotice('')
                                }
                              }}
                            >
                              {t('deleteSaved')}
                            </MenuItem>
                          </Menu>
                        </div>
                      </td>
                      <td>{parametersText(definition)}</td>
                      <td>{rangeText(definition)}</td>
                      <td>{actorName}</td>
                    </tr>
                  ))
                ) : (
                  <tr>
                    <td colSpan={4}>{t('savedEmpty')}</td>
                  </tr>
                )
              ) : (
                <tr>
                  <td colSpan={4} role={page.isError ? 'alert' : 'status'}>
                    {t(page.isError ? 'savedUnavailable' : 'savedLoading')}
                  </td>
                </tr>
              )}
            </tbody>
          </Table>
          <div className="flex flex-wrap gap-2 p-4">
            <Button
              variant="outline"
              size="sm"
              disabled={page.isFetching}
              onClick={() => {
                if (owner()) void page.refetch()
              }}
            >
              {t('refreshSaved')}
            </Button>
            <Button
              variant="outline"
              size="sm"
              disabled={!rows || !history.length}
              onClick={() => {
                if (owner()) {
                  setAfter(history[history.length - 1])
                  setHistory(history.slice(0, -1))
                }
              }}
            >
              {t('previousSaved')}
            </Button>
            <Button
              variant="outline"
              size="sm"
              disabled={!rows?.next_cursor}
              onClick={() => {
                if (owner() && rows?.next_cursor) {
                  setHistory([...history, after])
                  setAfter(rows.next_cursor)
                }
              }}
            >
              {t('nextSaved')}
            </Button>
          </div>
        </Card>
        <Dialog
          open={!!selected}
          onOpenChange={(open) => {
            if (!open && !deleteLock.current) {
              setSelected(null)
              setDeleteNotice('')
            }
          }}
          busy={deleting}
          title={t('deleteSavedTitle')}
          description={t('deleteSavedDescription')}
        >
          {selected && (
            <div className="space-y-4">
              <p>{title(selected.kind)}</p>
              <code className="block text-xs">{selected.id}</code>
              <p className="text-sm">{t('savedCreatedAt', { time: createdAt(selected) })}</p>
              <p className="text-sm">{rangeText(selected)}</p>
              <p className="text-sm">{parametersText(selected)}</p>
              {deleteNotice && <p role="alert">{t(deleteNotice)}</p>}
              <div className="flex justify-end gap-2">
                <Button
                  variant="outline"
                  disabled={deleting}
                  onClick={() => {
                    if (deleteLock.current) return
                    setSelected(null)
                    setDeleteNotice('')
                  }}
                >
                  {t('cancel')}
                </Button>
                <Button disabled={deleting} onClick={() => void remove()}>
                  {t(deleting ? 'deletingSaved' : 'confirmDeleteSaved')}
                </Button>
              </div>
            </div>
          )}
        </Dialog>
      </>
    ) : null,
  }
}

import { useState, useRef } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { RefreshCw } from 'lucide-react'
import {
  getRepositoryConfig,
  getRepositoryCandidates,
  previewRepositoryPrices,
  validRepositoryReason,
} from '@/api/repository-prices'
import type {
  RepositoryMapping,
  RepositoryPreview,
  RepositorySelection,
} from '@/types/repository-prices'
import { ResourceSection } from '@/views/resources/shared'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { Dialog } from '@/components/ui/dialog'
import { Table } from '@/components/ui/table'
import { useRepositoryAuthority, type RepositorySession } from './authority'
import { useRepositoryMutation } from './mutation'
import { RepositoryDifferenceDialog, RepositoryMutationDialogs } from './dialogs'
interface Draft {
  enabled: boolean
  mappings: RepositoryMapping[]
  etag: string
  sourceDigest: string
  reason: string
}
export function RepositoryPriceCard({ session }: { session: RepositorySession }) {
  return <RepositoryCardFrame key={session.data?.user.id ?? ''} session={session} />
}
function RepositoryCardFrame({ session }: { session: RepositorySession }) {
  const { t, i18n } = useTranslation('priceImports'),
    authority = useRepositoryAuthority(session, 'card')
  const query = useQuery({
    queryKey: authority.configKey,
    queryFn: ({ signal }) => getRepositoryConfig(signal),
    enabled: authority.readable,
    retry: false,
    staleTime: 0,
    gcTime: 0,
  })
  const view =
    authority.readable && query.isSuccess && !query.isFetching && !query.error
      ? query.data
      : undefined
  const writable = !!view?.can_write && authority.writable
  const [open, setOpen] = useState(false),
    [draft, setDraft] = useState<Draft>(),
    [issue, setIssue] = useState('')
  const [search, setSearch] = useState(''),
    [cursor, setCursor] = useState<string | null>(null),
    [cursors, setCursors] = useState<(string | null)[]>([])
  const [preview, setPreview] = useState<{
      data: RepositoryPreview
      selection: RepositorySelection
    }>(),
    [reason, setReason] = useState(''),
    [previewBusy, setPreviewBusy] = useState(false)
  const [selectionOpen, setSelectionOpen] = useState(false),
    [selected, setSelected] = useState<string[]>([])
  const lock = useRef(false)
  const mutation = useRepositoryMutation(authority, () => {
    setPreview(undefined)
    setOpen(false)
    setDraft(undefined)
    setIssue('')
    void query.refetch()
    void authority.cache.invalidateQueries({ queryKey: ['admin', 'prices'] })
  })
  const candidates = useQuery({
    queryKey: [
      'admin',
      'repository-prices',
      authority.actor,
      'candidates',
      authority.generation,
      search,
      cursor,
    ],
    queryFn: ({ signal }) => getRepositoryCandidates(search, cursor, signal),
    enabled: !!view && open && !mutation.unknown,
    retry: false,
    staleTime: 0,
    gcTime: 0,
  })
  const candidatePage =
    view && candidates.isSuccess && !candidates.isFetching && !candidates.error
      ? candidates.data
      : undefined
  function edit(enabled = view?.enabled) {
    if (!view || mutation.busy || mutation.unknown) return
    setDraft({
      enabled: !!enabled,
      mappings: structuredClone(view.mappings),
      etag: view.review_etag,
      sourceDigest: view.source.digest,
      reason: '',
    })
    setSearch('')
    setCursor(null)
    setCursors([])
    setIssue('')
    setOpen(true)
  }
  async function reviewConfig() {
    const valid = authority.readGuard()
    const result = await query.refetch()
    if (valid() && result.isSuccess && result.data) {
      setDraft(
        (current) =>
          current && {
            ...current,
            etag: result.data.review_etag,
            sourceDigest: result.data.source.digest,
          },
      )
      setIssue('')
      mutation.setNotice('')
    }
  }
  function mapping(modelId: string, source: string) {
    setDraft(
      (current) =>
        current && {
          ...current,
          mappings: source
            ? [
                ...current.mappings.filter((m) => m.provider_model_id !== modelId),
                { provider_model_id: modelId, source_model_key: source },
              ]
            : current.mappings.filter((m) => m.provider_model_id !== modelId),
        },
    )
  }
  async function reviewSelection(selection: RepositorySelection, refresh = false) {
    if (!authority.current() || !view || lock.current || mutation.unknown) return
    lock.current = true
    setPreviewBusy(true)
    setIssue('')
    const valid = authority.readGuard()
    if (refresh) {
      const refreshed = await query.refetch()
      if (!valid() || !refreshed.isSuccess) {
        lock.current = false
        setPreviewBusy(false)
        return
      }
    }
    const actor = authority.current()
    if (!actor) {
      lock.current = false
      setPreviewBusy(false)
      return
    }
    const operation = authority.begin()
    try {
      const result = await previewRepositoryPrices(
        selection,
        actor.csrf_token,
        operation.controller.signal,
      )
      if (operation.valid() && authority.current()) {
        setPreview({ data: result, selection: structuredClone(selection) })
        setSelectionOpen(false)
        mutation.setNotice('')
      }
    } catch {
      if (operation.valid()) setIssue('previewFailed')
    } finally {
      operation.release()
      lock.current = false
      setPreviewBusy(false)
    }
  }
  function sync() {
    if (!view?.enabled || !view.source.model_count || !view.mappings.length) return
    if (view.mappings.length > 20) {
      setSelected([])
      setSelectionOpen(true)
    } else
      void reviewSelection({
        mode: 'sync',
        provider_model_ids: view.mappings.map((m) => m.provider_model_id),
        rate_ids: [],
      })
  }
  function configure() {
    if (!draft || !view || !writable || mutation.unknown) return
    if (!validRepositoryReason(draft.reason)) {
      setIssue('reasonInvalid')
      return
    }
    if (draft.etag !== view.review_etag || draft.sourceDigest !== view.source.digest) {
      setIssue('conflict')
      return
    }
    mutation.prepare({
      kind: 'configure',
      etag: draft.etag,
      sourceDigest: draft.sourceDigest,
      input: {
        request_id: crypto.randomUUID(),
        enabled: draft.enabled,
        mappings: structuredClone(draft.mappings),
        reason: draft.reason,
      },
    })
  }
  function apply() {
    if (!preview || !view || !writable || mutation.unknown || !preview.data.valid) return
    if (!validRepositoryReason(reason)) {
      setIssue('reasonInvalid')
      return
    }
    if (
      preview.data.review_etag !== view.review_etag ||
      preview.data.source_digest !== view.source.digest
    ) {
      setIssue('conflict')
      return
    }
    mutation.prepare({
      kind: 'apply',
      etag: preview.data.review_etag,
      sourceDigest: preview.data.source_digest,
      input: {
        request_id: crypto.randomUUID(),
        preview_digest: preview.data.preview_digest,
        selection: structuredClone(preview.selection),
        reason,
      },
    })
  }
  const currentDraft =
    !!draft &&
    !!view &&
    draft.etag === view.review_etag &&
    draft.sourceDigest === view.source.digest
  const showIssue =
    issue || (['conflict', 'invalid', 'rejected'].includes(mutation.notice) ? mutation.notice : '')
  const date = (value: string | null) =>
    value
      ? new Date(value).toLocaleString(i18n.resolvedLanguage === 'zh' ? 'zh-CN' : 'en-US')
      : t('repository.never')
  return (
    <ResourceSection
      title={t('repository.title')}
      action={
        view && (
          <div className="flex items-center gap-2 text-sm">
            <span>{t('repository.enable')}</span>
            <Switch
              aria-label={t('repository.enable')}
              checked={view.enabled}
              disabled={!writable || mutation.busy || mutation.unknown}
              onCheckedChange={edit}
            />
          </div>
        )
      }
    >
      <div className="space-y-4">
        <div className="flex flex-wrap items-center justify-between gap-4">
          <p className="max-w-2xl text-sm text-muted-foreground">{t('repository.help')}</p>
          <Button
            variant="outline"
            disabled={
              !view?.enabled ||
              !view?.source.model_count ||
              !view.mappings.length ||
              previewBusy ||
              mutation.busy ||
              mutation.unknown
            }
            onClick={sync}
          >
            <RefreshCw className="size-4" aria-hidden="true" />
            {t('repository.preview')}
          </Button>
        </div>
        {!view ? (
          <p role="status">
            {t(
              authority.fresh &&
                ((!authority.readable && !authority.permissions.isFetching) || query.error)
                ? 'repository.loadFailed'
                : 'repository.loading',
            )}
          </p>
        ) : (
          <>
            <dl className="flex flex-wrap gap-x-8 gap-y-3 text-sm">
              <div>
                <dt className="inline text-muted-foreground">{t('repository.scope')}: </dt>
                <dd className="inline">
                  {t('repository.modelCount', { count: view.mappings.length })}{' '}
                  <Button
                    variant="ghost"
                    size="sm"
                    disabled={mutation.busy || mutation.unknown}
                    onClick={() => edit()}
                  >
                    {t('repository.editScope')}
                  </Button>
                </dd>
              </div>
              <div>
                <dt className="inline text-muted-foreground">{t('repository.lastSuccess')}: </dt>
                <dd className="inline">{date(view.last_success_at)}</dd>
              </div>
              <div>
                <dt className="inline text-muted-foreground">{t('repository.lastAttempt')}: </dt>
                <dd className="inline">{date(view.last_attempt_at)}</dd>
              </div>
              <div>
                <dt className="inline text-muted-foreground">{t('repository.result')}: </dt>
                <dd className="inline">
                  {view.last_result
                    ? t(
                        ['committed', 'no_changes'].includes(view.last_result)
                          ? `repository.${view.last_result}`
                          : 'repository.unknownResult',
                      )
                    : t('none')}
                </dd>
              </div>
            </dl>
            {!view.source.model_count && <p>{t('repository.emptySource')}</p>}
            {!view.mappings.length && <p>{t('repository.emptyScope')}</p>}
            <p className="break-all text-xs text-muted-foreground">
              {t('repository.sourceDigest')}: {view.source.digest}
            </p>
          </>
        )}
        <Button
          variant="outline"
          disabled={
            !authority.fresh ||
            query.isFetching ||
            authority.permissions.isFetching ||
            mutation.busy
          }
          onClick={() => {
            if (authority.readable) void query.refetch()
            else void authority.permissions.refetch()
          }}
        >
          {t('repository.refresh')}
        </Button>
        {showIssue && !preview && <p role="alert">{t(`repository.${showIssue}`)}</p>}
        <RepositoryMutationDialogs mutation={mutation} visible={!!view} writable={writable} />
      </div>
      <Dialog
        open={open && !mutation.unknown}
        onOpenChange={setOpen}
        title={t('repository.editScope')}
        description={t('repository.scopeHelp')}
        busy={mutation.busy}
        width={720}
      >
        {!view || !draft ? (
          <p role="status">{t('repository.loadFailed')}</p>
        ) : (
          <div className="space-y-4">
            <div className="flex items-center justify-between">
              <span>{t('repository.enable')}</span>
              <Switch
                aria-label={t('repository.enable')}
                checked={draft.enabled}
                disabled={!writable || mutation.unknown}
                onCheckedChange={(enabled) => setDraft({ ...draft, enabled })}
              />
            </div>
            <p className="text-sm text-muted-foreground">{t('repository.mappingHelp')}</p>
            {!!draft.mappings.length && (
              <Table aria-label={t('repository.scope')}>
                <thead>
                  <tr>
                    <th>{t('model')}</th>
                    <th>{t('repository.sourceModel')}</th>
                    <th>{t('repository.remove')}</th>
                  </tr>
                </thead>
                <tbody>
                  {draft.mappings.map((item) => (
                    <tr key={item.provider_model_id}>
                      <td>{item.provider_model_id}</td>
                      <td>{item.source_model_key}</td>
                      <td>
                        <Button
                          variant="ghost"
                          disabled={!writable || mutation.unknown}
                          onClick={() => mapping(item.provider_model_id, '')}
                        >
                          {t('repository.remove')}
                        </Button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </Table>
            )}
            <label className="block text-sm">
              {t('repository.search')}
              <Input
                className="mt-1"
                value={search}
                disabled={!writable || mutation.unknown}
                onChange={(event) => {
                  setSearch(event.target.value)
                  setCursor(null)
                  setCursors([])
                }}
              />
            </label>
            {candidates.isFetching && <p role="status">{t('repository.loading')}</p>}
            {candidates.error && <p role="alert">{t('repository.candidatesFailed')}</p>}
            {candidatePage && (
              <Table aria-label={t('repository.candidates')}>
                <thead>
                  <tr>
                    <th>{t('model')}</th>
                    <th>{t('repository.sourceModel')}</th>
                  </tr>
                </thead>
                <tbody>
                  {candidatePage.items.map((model) => (
                    <tr key={model.provider_model_id}>
                      <td>
                        {model.upstream_name}
                        <p className="text-xs">
                          {model.provider_model_id} · {model.protocol}
                        </p>
                      </td>
                      <td>
                        <select
                          aria-label={t('repository.mapLabel', { id: model.provider_model_id })}
                          className="h-10 max-w-64 rounded-md border bg-background px-3"
                          value={
                            draft.mappings.find(
                              (m) => m.provider_model_id === model.provider_model_id,
                            )?.source_model_key ?? ''
                          }
                          disabled={
                            !writable ||
                            mutation.unknown ||
                            (draft.mappings.length >= 100 &&
                              !draft.mappings.some(
                                (m) => m.provider_model_id === model.provider_model_id,
                              ))
                          }
                          onChange={(event) => mapping(model.provider_model_id, event.target.value)}
                        >
                          <option value="">{t('repository.unmapped')}</option>
                          {view.source.models
                            .filter((source) => source.protocol === model.protocol)
                            .map((source) => (
                              <option key={source.key} value={source.key}>
                                {source.model} · {source.key}
                              </option>
                            ))}
                        </select>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </Table>
            )}
            <div className="flex justify-end gap-2">
              <Button
                variant="outline"
                disabled={!cursors.length || candidates.isFetching}
                onClick={() => {
                  setCursor(cursors.at(-1) ?? null)
                  setCursors(cursors.slice(0, -1))
                }}
              >
                {t('previous')}
              </Button>
              <Button
                variant="outline"
                disabled={!candidatePage?.next_cursor || candidates.isFetching}
                onClick={() => {
                  setCursors([...cursors, cursor])
                  setCursor(candidatePage!.next_cursor)
                }}
              >
                {t('next')}
              </Button>
            </div>
            <label className="block text-sm">
              {t('repository.reason')}
              <Input
                className="mt-1"
                value={draft.reason}
                disabled={!writable || mutation.unknown}
                onChange={(event) => setDraft({ ...draft, reason: event.target.value })}
              />
            </label>
            {showIssue && <p role="alert">{t(`repository.${showIssue}`)}</p>}
            <Button
              variant="outline"
              disabled={mutation.busy || query.isFetching}
              onClick={() => void reviewConfig()}
            >
              {t('repository.reviewConfig')}
            </Button>
            <div className="flex justify-end gap-2">
              <Button variant="outline" onClick={() => setOpen(false)}>
                {t('cancel')}
              </Button>
              <Button
                disabled={
                  !writable ||
                  !currentDraft ||
                  mutation.busy ||
                  mutation.unknown ||
                  mutation.notice === 'conflict'
                }
                onClick={configure}
              >
                {t('repository.saveConfig')}
              </Button>
            </div>
          </div>
        )}
      </Dialog>
      <Dialog
        open={selectionOpen && !!view}
        onOpenChange={setSelectionOpen}
        title={t('repository.selectScope')}
        description={t('repository.batchHelp')}
        busy={previewBusy}
      >
        {view && (
          <div className="space-y-3">
            {view.mappings.map((mapping) => (
              <label key={mapping.provider_model_id} className="flex items-center gap-2 text-sm">
                <input
                  type="checkbox"
                  checked={selected.includes(mapping.provider_model_id)}
                  disabled={selected.length >= 20 && !selected.includes(mapping.provider_model_id)}
                  onChange={(event) =>
                    setSelected(
                      event.target.checked
                        ? [...selected, mapping.provider_model_id]
                        : selected.filter((id) => id !== mapping.provider_model_id),
                    )
                  }
                />
                {mapping.provider_model_id} → {mapping.source_model_key}
              </label>
            ))}
            <Button
              disabled={!selected.length || previewBusy}
              onClick={() =>
                void reviewSelection({ mode: 'sync', provider_model_ids: selected, rate_ids: [] })
              }
            >
              {t('repository.preview')}
            </Button>
          </div>
        )}
      </Dialog>
      {preview && view && !mutation.unknown && (
        <RepositoryDifferenceDialog
          key={preview.data.preview_digest || preview.data.review_etag}
          preview={preview.data}
          busy={previewBusy || mutation.busy}
          canApply={
            writable &&
            mutation.notice !== 'conflict' &&
            preview.data.valid &&
            preview.data.changes.some((c) => ['added', 'updated'].includes(c.action)) &&
            preview.data.review_etag === view.review_etag &&
            preview.data.source_digest === view.source.digest
          }
          notice={showIssue}
          reason={reason}
          onReason={setReason}
          onApply={apply}
          onReview={() => void reviewSelection(preview.selection, true)}
          onClose={() => setPreview(undefined)}
        />
      )}
    </ResourceSection>
  )
}

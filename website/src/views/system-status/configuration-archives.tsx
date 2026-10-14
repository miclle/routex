import { useLayoutEffect, useRef, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { getRuntimeArchives } from '@/api/runtime-archives'
import { locale } from '@/i18n'
import { Button } from '@/components/ui/button'
import { Dialog } from '@/components/ui/dialog'
import { Table } from '@/components/ui/table'

export function ConfigurationArchivesDialog({
  actorID,
  authority,
  isCurrent,
  onClose,
}: {
  actorID: string
  authority: string
  isCurrent: () => boolean
  onClose: () => void
}) {
  const { t } = useTranslation('systemStatus')
  const cache = useQueryClient()
  const [cursor, setCursor] = useState<string>()
  const lifetime = useRef<{ cursor: typeof cursor } | null>(null)
  useLayoutEffect(() => {
    lifetime.current = { cursor }
    return () => {
      lifetime.current = null
    }
  }, [cursor])
  const cursorCurrent = () =>
    isCurrent() && lifetime.current !== null && lifetime.current.cursor === cursor
  const queryKey = ['admin', 'runtime-archives', actorID, authority, cursor]
  const query = useQuery({
    queryKey,
    enabled: isCurrent(),
    retry: false,
    gcTime: 0,
    refetchOnWindowFocus: false,
    queryFn: async ({ signal }) => {
      if (!cursorCurrent() || signal.aborted) throw new DOMException('Obsolete read', 'AbortError')
      const page = await getRuntimeArchives(cursor ? { cursor } : {}, signal)
      if (!cursorCurrent() || signal.aborted) throw new DOMException('Obsolete read', 'AbortError')
      return page
    },
  })
  const readBasis = () => {
    const state = cache.getQueryState(queryKey)
    return JSON.stringify([state?.status, state?.dataUpdateCount, state?.errorUpdateCount])
  }
  const generation = readBasis()
  function readCurrent() {
    return (
      cursorCurrent() &&
      cache.getQueryState(queryKey)?.fetchStatus === 'idle' &&
      readBasis() === generation
    )
  }
  const visible = query.isSuccess && !query.isFetching && !query.isError && isCurrent()
  function visibleCurrent() {
    const state = cache.getQueryState(queryKey)
    return (
      visible &&
      cursorCurrent() &&
      state?.status === 'success' &&
      state.fetchStatus === 'idle' &&
      !state.isInvalidated &&
      readCurrent()
    )
  }
  function move(next?: string) {
    if (!visibleCurrent()) return
    lifetime.current = null
    setCursor(next)
  }
  return (
    <Dialog
      open={isCurrent()}
      onOpenChange={(open) => {
        if (!open) {
          lifetime.current = null
          onClose()
        }
      }}
      title={t('archiveMetadataTitle')}
      description={t('archiveMetadataBoundary')}
      width={880}
    >
      <p className="mb-4 text-sm text-muted-foreground">{t('archiveCaptureBoundary')}</p>
      {query.isFetching && <p role="status">{t('refreshing')}</p>}
      {query.isError && isCurrent() && (
        <p role="alert" className="text-destructive">
          {t('archiveMetadataFailed')}
        </p>
      )}
      {visible && query.data.items.length === 0 && <p role="status">{t('archiveMetadataEmpty')}</p>}
      {visible && query.data.items.length > 0 && (
        <Table aria-label={t('archiveMetadataTable')}>
          <thead>
            <tr>
              {[
                'archiveIdentity',
                'archiveCreatedAt',
                'archiveCodec',
                'archiveContentDigest',
                'archiveDocumentBytes',
              ].map((column) => (
                <th key={column}>{t(column)}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {query.data.items.map((row) => (
              <tr key={row.id}>
                <td className="break-all font-mono text-xs">{row.id}</td>
                <td className="whitespace-nowrap">
                  {new Intl.DateTimeFormat(locale(), {
                    dateStyle: 'medium',
                    timeStyle: 'short',
                  }).format(new Date(row.created_at))}
                </td>
                <td>{row.codec_version}</td>
                <td className="break-all font-mono text-xs">{row.content_digest}</td>
                <td>{new Intl.NumberFormat(locale()).format(row.document_bytes)}</td>
              </tr>
            ))}
          </tbody>
        </Table>
      )}
      <div className="mt-4 flex flex-wrap justify-end gap-2">
        <Button
          variant="outline"
          disabled={query.isFetching || !isCurrent()}
          onClick={() => {
            if (!readCurrent() || query.isFetching) return
            void query.refetch()
          }}
        >
          {t('refresh')}
        </Button>
        <Button
          variant="outline"
          disabled={!visible || cursor === undefined}
          onClick={() => move()}
        >
          {t('archiveFirstPage')}
        </Button>
        <Button
          variant="outline"
          disabled={!visible || !query.data?.next_cursor}
          onClick={() => {
            if (query.data?.next_cursor) move(query.data.next_cursor)
          }}
        >
          {t('archiveNextPage')}
        </Button>
        <Button
          variant="outline"
          onClick={() => {
            lifetime.current = null
            onClose()
          }}
        >
          {t('routingClose')}
        </Button>
      </div>
    </Dialog>
  )
}

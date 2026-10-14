import { useCallback, useLayoutEffect, useRef, useState, useSyncExternalStore } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { getRuntimeArchives } from '@/api/runtime-archives'
import { locale } from '@/i18n'
import { Button } from '@/components/ui/button'
import { Dialog } from '@/components/ui/dialog'
import { Table } from '@/components/ui/table'
import { Switch } from '@/components/ui/switch'
import { ArchiveCleanup } from './archive-cleanup'
import { ArchiveValidation } from './archive-validation'
import { ArchiveRollback } from './archive-rollback'
import { sessionKey } from '@/hooks/use-auth'
import type { Session } from '@/types/auth'

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
  const [selected, setSelected] = useState<string[]>([])
  const [cleanupLocked, setCleanupLocked] = useState(false)
  const cleanupLock = useRef(false)
  const rollbackLock = useRef(false)
  const [rollbackTarget, setRollbackTarget] = useState<{ id: string; birth: string } | null>(null)
  const locked = cleanupLocked || rollbackTarget !== null
  const lockedCurrent = () => cleanupLock.current || rollbackLock.current
  useLayoutEffect(() => {
    rollbackLock.current = rollbackTarget !== null
  }, [rollbackTarget])
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
  const readBasis = useCallback(() => {
    const state = cache.getQueryState(['admin', 'runtime-archives', actorID, authority, cursor])
    return JSON.stringify([
      state?.status,
      state?.fetchStatus,
      state?.isInvalidated,
      state?.dataUpdateCount,
      state?.errorUpdateCount,
    ])
  }, [cache, actorID, authority, cursor])
  const subscribe = useCallback(
    (changed: () => void) => cache.getQueryCache().subscribe(changed),
    [cache],
  )
  const generation = useSyncExternalStore(subscribe, readBasis, readBasis)
  function readCurrent() {
    return (
      cursorCurrent() &&
      cache.getQueryState(queryKey)?.fetchStatus === 'idle' &&
      readBasis() === generation
    )
  }
  const visible =
    query.isSuccess &&
    !query.isFetching &&
    !query.isError &&
    isCurrent() &&
    !cache.getQueryState(queryKey)?.isInvalidated &&
    readBasis() === generation
  if (
    visible &&
    rollbackTarget &&
    query.data.items.some(
      (row) => row.id === rollbackTarget.id && row.created_at !== rollbackTarget.birth,
    )
  )
    setRollbackTarget(null)
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
    if (!visibleCurrent() || lockedCurrent()) return
    lifetime.current = null
    setSelected([])
    setCursor(next)
  }
  function canCleanup() {
    const actor = cache.getQueryData<Session>(sessionKey)?.user
    const permissions = cache.getQueryData<string[]>(['permissions', actorID])
    return (
      visibleCurrent() &&
      !rollbackLock.current &&
      actor?.id === actorID &&
      actor.role === 'admin' &&
      permissions?.includes('system.write') === true
    )
  }
  function canValidate() {
    const actor = cache.getQueryData<Session>(sessionKey)?.user
    const permissions = cache.getQueryData<string[]>(['permissions', actorID])
    return (
      visibleCurrent() &&
      actor?.id === actorID &&
      actor.role === 'admin' &&
      permissions?.includes('system.read') === true
    )
  }
  function canRollback(write = false) {
    const actor = cache.getQueryData<Session>(sessionKey)?.user
    const permissions = cache.getQueryData<string[]>(['permissions', actorID])
    const required = write
      ? [
          'system.read',
          'providers.read',
          'models.read_all',
          'system.write',
          'providers.write',
          'models.write',
        ]
      : ['system.read', 'providers.read', 'models.read_all']
    return (
      visibleCurrent() &&
      actor?.id === actorID &&
      actor.role === 'admin' &&
      required.every((permission) => permissions?.includes(permission))
    )
  }
  function targetCurrent() {
    return (
      !!rollbackTarget &&
      canRollback() &&
      query.data?.items.some(
        (row) => row.id === rollbackTarget.id && row.created_at === rollbackTarget.birth,
      ) === true
    )
  }
  const actor = cache.getQueryData<Session>(sessionKey)?.user
  const permissions = cache.getQueryData<string[]>(['permissions', actorID])
  const currentAdmin = visible && actor?.id === actorID && actor.role === 'admin'
  const cleanupVisible = currentAdmin && permissions?.includes('system.write') === true
  const validationVisible = currentAdmin && permissions?.includes('system.read') === true
  const rollbackVisible =
    currentAdmin &&
    ['system.read', 'providers.read', 'models.read_all'].every((permission) =>
      permissions?.includes(permission),
    )
  return (
    <Dialog
      open={true}
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
      <p className="mb-4 text-sm text-muted-foreground">{t('archiveValidationBoundary')}</p>
      {(query.isFetching || !isCurrent()) && <p role="status">{t('refreshing')}</p>}
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
              {cleanupVisible && <th>{t('archiveCleanupSelect')}</th>}
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
                {cleanupVisible && (
                  <td>
                    <Switch
                      checked={selected.includes(row.id)}
                      disabled={locked || (!selected.includes(row.id) && selected.length >= 20)}
                      aria-label={t('archiveCleanupSelectIdentity', { id: row.id })}
                      onCheckedChange={(checked) => {
                        if (!canCleanup() || lockedCurrent()) return
                        setSelected((current) =>
                          checked
                            ? [...current, row.id].sort()
                            : current.filter((id) => id !== row.id),
                        )
                      }}
                    />
                  </td>
                )}
                <td className="break-all font-mono text-xs">
                  {row.id}
                  {validationVisible && (
                    <ArchiveValidation
                      key={JSON.stringify([actorID, authority, generation, row.id, row.created_at])}
                      actorID={actorID}
                      authority={JSON.stringify([authority, generation])}
                      archiveID={row.id}
                      archiveBirth={row.created_at}
                      isCurrent={() => canValidate() && !lockedCurrent()}
                    />
                  )}
                  {rollbackVisible && (
                    <div className="mt-2">
                      <Button
                        size="sm"
                        variant="outline"
                        disabled={locked}
                        aria-label={t('archiveRollbackIdentity', { id: row.id })}
                        onClick={() => {
                          if (!canRollback() || lockedCurrent()) return
                          rollbackLock.current = true
                          setRollbackTarget({ id: row.id, birth: row.created_at })
                        }}
                      >
                        {t('archiveRollbackAction')}
                      </Button>
                    </div>
                  )}
                </td>
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
      {rollbackTarget && (
        <ArchiveRollback
          key={JSON.stringify([actorID, rollbackTarget.id, rollbackTarget.birth])}
          actorID={actorID}
          authority={JSON.stringify([authority, generation])}
          archiveID={rollbackTarget.id}
          archiveBirth={rollbackTarget.birth}
          canRead={targetCurrent}
          canWrite={() => targetCurrent() && canRollback(true)}
          onClose={() => {
            rollbackLock.current = false
            setRollbackTarget(null)
          }}
        />
      )}
      <ArchiveCleanup
        authority={JSON.stringify([authority, generation])}
        selected={selected}
        isCurrent={visibleCurrent}
        canWrite={canCleanup}
        onLock={(locked) => {
          cleanupLock.current = locked
          setCleanupLocked(locked)
        }}
        onComplete={() => {
          setSelected([])
          void query.refetch()
        }}
      />
      <div className="mt-4 flex flex-wrap justify-end gap-2">
        <Button
          variant="outline"
          disabled={query.isFetching || !isCurrent() || locked}
          onClick={() => {
            if (!readCurrent() || query.isFetching || lockedCurrent()) return
            void query.refetch()
          }}
        >
          {t('refresh')}
        </Button>
        <Button
          variant="outline"
          disabled={!visible || cursor === undefined || locked}
          onClick={() => move()}
        >
          {t('archiveFirstPage')}
        </Button>
        <Button
          variant="outline"
          disabled={!visible || !query.data?.next_cursor || locked}
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

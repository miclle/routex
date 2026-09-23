import { useRef, useState, type ChangeEvent, type DragEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { useQueryClient } from '@tanstack/react-query'
import { Link } from 'react-router'
import axios from 'axios'
import { Download, Inbox } from 'lucide-react'
import {
  previewPriceCSV,
  commitPriceCSV,
  exportPriceCSV,
  downloadPriceCSV,
} from '@/api/price-imports'
import { useSession } from '@/hooks/use-auth'
import { usePermissions } from '@/hooks/use-permissions'
import { PermissionGate } from '@/components/app/PermissionGate'
import { Page, ErrorNotice } from '@/components/app/CatalogUI'
import { Button } from '@/components/ui/button'
import { Dialog } from '@/components/ui/dialog'
import { ResourceSection } from '@/views/resources/shared'
import type { PriceImportPreview, SelectedPriceFile } from '@/types/price-imports'
import { readPriceFile, PriceFileError } from './file'
import { PricePreviewDialog } from './preview'
export default function PriceImportsPage() {
  return (
    <PermissionGate permission="prices.read">
      <PriceImports />
    </PermissionGate>
  )
}
function PriceImports() {
  const { t } = useTranslation('priceImports')
  const session = useSession()
  const access = usePermissions()
  const cache = useQueryClient()
  const [file, setFile] = useState<SelectedPriceFile | null>(null)
  const [preview, setPreview] = useState<{
    file: SelectedPriceFile
    result: PriceImportPreview
  } | null>(null)
  const [busy, setBusy] = useState('')
  const lock = useRef(false)
  const generation = useRef(0)
  const [issue, setIssue] = useState('')
  const [error, setError] = useState<unknown>(null)
  const [notice, setNotice] = useState('')
  const [apiOpen, setApiOpen] = useState(false)
  async function select(files: FileList | null) {
    if (lock.current || !files?.length) return
    const turn = ++generation.current
    setFile(null)
    setPreview(null)
    setIssue('')
    setError(null)
    setNotice('')
    if (files.length !== 1) {
      setIssue('oneFile')
      return
    }
    setBusy('reading')
    try {
      const csv = await readPriceFile(files[0])
      if (turn === generation.current) setFile({ name: files[0].name, csv })
    } catch (error) {
      if (turn === generation.current)
        setIssue(error instanceof PriceFileError ? error.message : 'fileRead')
    } finally {
      if (turn === generation.current) setBusy('')
    }
  }
  async function review(captured = file) {
    if (!captured || lock.current) return
    lock.current = true
    setBusy('previewing')
    setPreview(null)
    setIssue('')
    setError(null)
    setNotice('')
    try {
      setPreview({
        file: captured,
        result: await previewPriceCSV(captured.csv, session.data!.csrf_token),
      })
    } catch (error) {
      setError(error)
    } finally {
      lock.current = false
      setBusy('')
    }
  }
  async function apply() {
    if (
      !preview ||
      lock.current ||
      issue ||
      !preview.result.valid ||
      !preview.result.changes.length ||
      !access.can('prices.write')
    )
      return
    const captured = preview
    lock.current = true
    setBusy('applying')
    setError(null)
    try {
      await commitPriceCSV(captured.file.csv, captured.result, session.data!.csrf_token)
      setPreview(null)
      setFile(null)
      setNotice('saved')
      void cache.invalidateQueries({ queryKey: ['admin', 'prices'] })
      void cache.invalidateQueries({ queryKey: ['admin', 'pricing-currency'] })
    } catch (error) {
      const status = axios.isAxiosError(error) ? error.response?.status : undefined
      if (status === 422 && axios.isAxiosError(error) && error.response?.data?.preview) {
        setPreview({ file: captured.file, result: error.response.data.preview })
        setIssue('invalidCommit')
      } else setIssue(status === 409 ? 'conflict' : status === 503 ? 'uncertain' : 'commitFailed')
    } finally {
      lock.current = false
      setBusy('')
    }
  }
  async function download() {
    if (lock.current) return
    lock.current = true
    setBusy('exporting')
    setError(null)
    setNotice('')
    try {
      downloadPriceCSV(await exportPriceCSV())
      setNotice('downloaded')
    } catch (error) {
      setError(error)
    } finally {
      lock.current = false
      setBusy('')
    }
  }
  const change = (event: ChangeEvent<HTMLInputElement>) => {
    void select(event.target.files)
    event.target.value = ''
  }
  const drop = (event: DragEvent) => {
    event.preventDefault()
    void select(event.dataTransfer.files)
  }
  return (
    <Page title={t('title')} description={t('description')}>
      <p className="text-sm text-muted-foreground">
        {t('navigation')}{' '}
        {access.can('providers.read') && (
          <Link className="text-primary underline-offset-4 hover:underline" to="/admin/providers">
            {t('providers')}
          </Link>
        )}
      </p>
      <ResourceSection
        title={t('uploadTitle')}
        action={
          <Button variant="outline" onClick={() => setApiOpen(true)}>
            {t('api')}
          </Button>
        }
      >
        <div className="space-y-6">
          <div className="flex flex-wrap items-center justify-between gap-4">
            <div className="space-y-2">
              <h3 className="text-sm font-semibold">{t('downloadStep')}</h3>
              <p className="text-sm text-muted-foreground">{t('downloadHelp')}</p>
            </div>
            <Button variant="outline" disabled={!!busy} onClick={() => void download()}>
              <Download className="size-4" />
              {t('download')}
            </Button>
          </div>
          <hr />
          <div className="space-y-2">
            <h3 className="text-sm font-semibold">{t('uploadStep')}</h3>
            <p className="text-sm text-muted-foreground">{t('uploadHelp')}</p>
          </div>
          <label
            className="relative flex min-h-44 cursor-pointer flex-col items-center justify-center gap-3 rounded-lg border border-dashed bg-muted/20 p-6 text-center hover:bg-muted/40"
            onDragOver={(event) => event.preventDefault()}
            onDrop={drop}
          >
            <Inbox className="size-9 text-muted-foreground" />
            <span className="text-sm font-medium">{t('drop')}</span>
            <span className="text-xs text-muted-foreground">{t('bounds')}</span>
            <input
              aria-label={t('fileInput')}
              type="file"
              accept=".csv,text/csv"
              disabled={!!busy}
              onChange={change}
              className="absolute inset-0 h-full w-full cursor-pointer opacity-0"
            />
          </label>
          {file && (
            <p className="break-all text-sm">
              {t('selected')}: {file.name}
            </p>
          )}
          {issue && !preview && (
            <p role="alert" className="text-sm text-destructive">
              {t(issue)}
            </p>
          )}
          <ErrorNotice error={error} />
          {notice && (
            <p role="status" className="text-sm">
              {t(notice)}
            </p>
          )}
          <div className="flex justify-end">
            <Button disabled={!file || !!busy} onClick={() => void review()}>
              {busy ? t('working') : t('validate')}
            </Button>
          </div>
        </div>
      </ResourceSection>
      {preview && (
        <PricePreviewDialog
          key={preview.result.etag + preview.result.preview_digest}
          preview={preview.result}
          name={preview.file.name}
          busy={!!busy}
          issue={issue}
          canApply={access.can('prices.write')}
          onClose={() => {
            setPreview(null)
          }}
          onApply={() => void apply()}
          onReview={() => void review(preview.file)}
        />
      )}
      <Dialog
        open={apiOpen}
        onOpenChange={setApiOpen}
        title={t('apiTitle')}
        description={t('apiHelp')}
        width={720}
      >
        <div className="space-y-4 text-sm">
          <p>{t('apiAuth')}</p>
          <pre className="overflow-auto rounded-md bg-muted p-4 text-xs">
            {
              'POST /api/v1/admin/prices/import/preview\n{ "csv": "..." }\n\nPOST /api/v1/admin/prices/import/commit\n{ "csv": "...", "etag": "...", "preview_digest": "..." }\n\nGET /api/v1/admin/prices/export.csv'
            }
          </pre>
          <p>{t('headers')}</p>
          <p className="font-mono text-xs break-all">
            provider_model_id,metric,tier,unit,currency,amount,enabled,context_threshold
          </p>
          <p>{t('identity')}</p>
        </div>
      </Dialog>
    </Page>
  )
}

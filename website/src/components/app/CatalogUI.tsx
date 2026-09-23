import { t } from '@/i18n'
import { useTranslation } from 'react-i18next'
import type { ReactNode } from 'react'
import { useSession } from '@/hooks/use-auth'
import { Button } from '@/components/ui/button'
import { catalogError } from '@/api/catalog'

export function AdminOnly({ children }: { children: ReactNode }) {
  useTranslation()

  const { data } = useSession()
  return data?.user.role === 'admin' ? (
    children
  ) : (
    <Page
      title={t('access_denied_cb8d4')}
      description={t('this_page_is_available_only_to_platform_administrators_2bcb6')}
    />
  )
}
export function Page({
  title,
  description,
  action,
  children,
}: {
  title: string
  description: string
  action?: ReactNode
  children?: ReactNode
}) {
  useTranslation()

  return (
    <section className="space-y-6">
      <header className={action ? 'flex justify-end' : 'sr-only'}>
        <div className="sr-only">
          <h1>{title}</h1>
          <p>{description}</p>
        </div>
        {action}
      </header>
      {children}
    </section>
  )
}
export function QueryState({
  pending,
  error,
  retry,
  empty,
}: {
  pending: boolean
  error: unknown
  retry: () => void
  empty?: boolean
}) {
  useTranslation()

  if (pending)
    return (
      <p role="status" className="py-8 text-sm text-muted-foreground">
        {t('loading_1d088')}
      </p>
    )
  if (error)
    return (
      <div className="space-y-3 rounded-lg border p-5">
        <ErrorNotice error={error} />
        <Button variant="outline" onClick={retry}>
          {t('retry_e2d53')}
        </Button>
      </div>
    )
  return empty ? (
    <p className="rounded-lg border border-dashed p-8 text-center text-sm text-muted-foreground">
      {t('no_data_available_2b024')}
    </p>
  ) : null
}
export function ErrorNotice({ error }: { error: unknown }) {
  useTranslation()
  return error ? (
    <p
      role="alert"
      className="rounded-md border border-destructive/30 bg-destructive/5 p-3 text-sm text-destructive"
    >
      {catalogError(error)}
    </p>
  ) : null
}
export function FormField({ label, children }: { label: string; children: ReactNode }) {
  useTranslation()
  return (
    <label className="block space-y-2 text-sm font-medium">
      <span>{label}</span>
      {children}
    </label>
  )
}
export function SaveButton({
  pending,
  disabled = false,
  children = t('save_fadf2'),
}: {
  pending: boolean
  disabled?: boolean
  children?: ReactNode
}) {
  useTranslation()
  return (
    <Button type="submit" disabled={pending || disabled}>
      {pending ? t('submitting_10b2d') : children}
    </Button>
  )
}

import { useTranslation } from 'react-i18next'
import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Mail } from 'lucide-react'
import { getRegistration } from '@/api/governance'
import { writeCatalog } from '@/api/catalog'
import { useSession } from '@/hooks/use-auth'
import { PermissionGate } from '@/components/app/PermissionGate'
import { Page, QueryState, ErrorNotice, SaveButton } from '@/components/app/CatalogUI'
import { Button } from '@/components/ui/button'
import { Drawer } from '@/components/ui/drawer'
import { Switch } from '@/components/ui/switch'
import { Badge } from '@/components/ui/badge'
export default function RegistrationPage() {
  return (
    <PermissionGate permission="registration.write">
      <Registration />
    </PermissionGate>
  )
}
function Registration() {
  const { t } = useTranslation('governance')
  const session = useSession()
  const cache = useQueryClient()
  const settings = useQuery({
    queryKey: ['admin', 'registration'],
    queryFn: () => getRegistration(true),
  })
  const [open, setOpen] = useState(false)
  const [enabled, setEnabled] = useState(false)
  const [notice, setNotice] = useState<'registration.saved' | null>(null)
  const mutation = useMutation({
    mutationFn: (enabled: boolean) =>
      writeCatalog<{ enabled: boolean }>(
        'patch',
        '/admin/registration',
        { enabled },
        session.data!.csrf_token,
      ),
    onSuccess: (result) => {
      cache.setQueryData(['admin', 'registration'], result)
      void cache.invalidateQueries({ queryKey: ['auth', 'registration'] })
      setNotice('registration.saved')
      setOpen(false)
    },
  })
  return (
    <Page title={t('registration.title')} description={t('registration.description')}>
      <div>
        <h2 className="text-base font-semibold">{t('registration.methods')}</h2>
        <p className="mt-2 text-sm text-muted-foreground">{t('registration.methodsDescription')}</p>
      </div>
      {notice && (
        <p role="status" className="text-sm">
          {t(notice)}
        </p>
      )}
      <QueryState
        pending={settings.isPending}
        error={settings.error}
        retry={() => void settings.refetch()}
      />
      {settings.data && (
        <section className="flex items-center justify-between gap-6 rounded-lg border p-4">
          <div className="flex items-center gap-4">
            <Mail className="size-6" />
            <div>
              <p className="flex items-center gap-2 font-medium">
                {t('registration.emailRegistration')}
                <Badge variant="outline">
                  {settings.data.enabled ? t('registration.enabled') : t('registration.notEnabled')}
                </Badge>
              </p>
              <p className="mt-2 text-sm text-muted-foreground">
                {t('registration.emailDescription')}
              </p>
            </div>
          </div>
          <Button
            variant="outline"
            aria-label={t('registration.configureLabel')}
            onClick={() => {
              mutation.reset()
              setEnabled(settings.data?.enabled ?? false)
              setOpen(true)
            }}
          >
            {t('registration.configure')}
          </Button>
        </section>
      )}
      <Drawer
        open={open}
        onOpenChange={setOpen}
        title={t('registration.configureLabel')}
        busy={mutation.isPending}
      >
        <form
          className="space-y-6"
          aria-label={t('registration.formLabel')}
          onSubmit={(event) => {
            event.preventDefault()
            if (mutation.isPending) return
            mutation.mutate(enabled)
          }}
        >
          <label className="flex items-center justify-between gap-4">
            <span>
              <span className="block text-sm font-medium">
                {t('registration.openRegistration')}
              </span>
              <span className="text-sm text-muted-foreground">
                {t('registration.memberNotice')}
              </span>
            </span>
            <Switch
              name="enabled"
              aria-label={t('registration.openRegistration')}
              checked={enabled}
              onCheckedChange={setEnabled}
              disabled={mutation.isPending}
            />
          </label>
          <ErrorNotice error={mutation.error} />
          <div className="flex justify-end">
            <SaveButton pending={mutation.isPending}>{t('registration.save')}</SaveButton>
          </div>
        </form>
      </Drawer>
    </Page>
  )
}

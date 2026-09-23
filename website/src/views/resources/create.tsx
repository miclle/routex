import { t } from '@/i18n'
import { useTranslation } from 'react-i18next'
import { useState, type FormEvent } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from 'react-router'
import { writeCatalog } from '@/api/catalog'
import { resourcePath } from '@/api/resources'
import { useSession } from '@/hooks/use-auth'
import { PermissionGate } from '@/components/app/PermissionGate'
import { Page, ErrorNotice, FormField, SaveButton } from '@/components/app/CatalogUI'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { CandidatePicker, ResourceSection } from './shared'
import type { ResourceKind, ResourceRecord } from '@/types/resources'
export default function CreateResourcePage({
  kind,
  admin = false,
}: {
  kind: ResourceKind
  admin?: boolean
}) {
  useTranslation()

  return kind === 'teams' ? (
    <PermissionGate permission="teams.write">
      <CreateResource kind={kind} admin={admin} />
    </PermissionGate>
  ) : (
    <CreateResource kind={kind} admin={admin} />
  )
}
function CreateResource({ kind, admin }: { kind: ResourceKind; admin: boolean }) {
  useTranslation()

  const session = useSession()
  const navigate = useNavigate()
  const cache = useQueryClient()
  const label = t(kind === 'teams' ? 'resources:team' : 'resources:project')
  const [owners, setOwners] = useState<string[]>([])
  const [validation, setValidation] = useState('')
  const create = useMutation({
    mutationFn: (input: { name: string; description: string; owner_ids?: string[] }) =>
      writeCatalog<ResourceRecord>(
        'post',
        kind === 'teams' ? '/admin/teams' : '/projects',
        input,
        session.data!.csrf_token,
      ),
    onSuccess: (resource) => {
      void cache.invalidateQueries({ queryKey: ['resources'] })
      navigate(`${resourcePath(kind, admin)}/${resource.id}`)
    },
  })
  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (create.isPending) return
    if (kind === 'teams' && !owners.length) {
      setValidation('select_at_least_one_team_owner_2f490')
      return
    }
    setValidation('')
    const form = new FormData(event.currentTarget)
    create.mutate({
      name: String(form.get('name')).trim(),
      description: String(form.get('description') || '').trim(),
      ...(kind === 'teams' ? { owner_ids: owners } : {}),
    })
  }
  return (
    <Page
      title={t('create_value_91d65', { v0: label })}
      description={t('create_value_and_define_responsibilities_4272c', { v0: label })}
    >
      <div className={kind === 'teams' ? 'max-w-[960px]' : ''}>
        <ResourceSection
          title={kind === 'teams' ? t('basic_information_b122f') : t('create_project_80f5d')}
        >
          <form
            aria-label={t('create_value_91d65', { v0: label })}
            onSubmit={submit}
            className="space-y-6"
          >
            <fieldset disabled={create.isPending} className="space-y-6">
              <FormField label={t('value_name_0e54c', { v0: label })}>
                <Input
                  name="name"
                  autoFocus
                  required
                  maxLength={100}
                  placeholder={
                    kind === 'teams' ? t('for_example_search_evaluation_11e97') : undefined
                  }
                />
              </FormField>
              <FormField
                label={kind === 'teams' ? t('description_26670') : t('business_description_aaa4b')}
              >
                <Textarea name="description" rows={3} maxLength={2000} />
              </FormField>
              {kind === 'teams' ? (
                <CandidatePicker
                  path="/admin/team-member-candidates"
                  selected={owners}
                  onChange={setOwners}
                  label={t('team_owners_f39f5')}
                  disabled={create.isPending}
                />
              ) : (
                <FormField label={t('project_managers_90131')}>
                  <Input
                    value={`${session.data?.user.name ?? ''} · ${session.data?.user.email ?? ''}`}
                    readOnly
                  />
                  <p className="text-xs font-normal text-muted-foreground">
                    {t('the_creator_is_the_initial_manager_add_other_c51f8')}
                  </p>
                </FormField>
              )}
              <p className="text-sm text-muted-foreground">
                {t('resources:newHelp', { kind: label })}
              </p>
            </fieldset>
            {validation && (
              <p role="alert" className="text-sm text-destructive">
                {t(validation)}
              </p>
            )}
            <ErrorNotice error={create.error} />
            <div className="flex justify-end gap-3">
              <Button
                type="button"
                variant="outline"
                disabled={create.isPending}
                onClick={() => navigate(resourcePath(kind, admin))}
              >
                {t('cancel_4d0b4')}
              </Button>
              <SaveButton pending={create.isPending}>
                {t('resources:create', { kind: label })}
              </SaveButton>
            </div>
          </form>
        </ResourceSection>
      </div>
    </Page>
  )
}

import { t } from '@/i18n'
import { useTranslation } from 'react-i18next'
import { useLayoutEffect, useRef, useState, type FormEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate } from 'react-router'
import { writeCatalog } from '@/api/catalog'
import { createProject, getProjectCreationManagers, resourcePath } from '@/api/resources'
import { useSession, sessionKey } from '@/hooks/use-auth'
import { usePermissions } from '@/hooks/use-permissions'
import type { Session } from '@/types/auth'
import { isAxiosError } from 'axios'
import { PermissionGate } from '@/components/app/PermissionGate'
import { Page, ErrorNotice, FormField, SaveButton, QueryState } from '@/components/app/CatalogUI'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { CandidatePicker, ResourceSection } from './shared'
import type { ResourceCandidate, ResourceKind, ResourceRecord } from '@/types/resources'
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
    <CreateProjectPage admin={admin} />
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
      title={t('common:create_value_91d65', { v0: label })}
      description={t('common:create_value_and_define_responsibilities_4272c', { v0: label })}
    >
      <div className={kind === 'teams' ? 'max-w-[960px]' : ''}>
        <ResourceSection
          title={
            kind === 'teams'
              ? t('common:basic_information_b122f')
              : t('common:create_project_80f5d')
          }
        >
          <form
            aria-label={t('common:create_value_91d65', { v0: label })}
            onSubmit={submit}
            className="space-y-6"
          >
            <fieldset disabled={create.isPending} className="space-y-6">
              <FormField label={t('common:value_name_0e54c', { v0: label })}>
                <Input
                  name="name"
                  autoFocus
                  required
                  maxLength={100}
                  placeholder={
                    kind === 'teams' ? t('common:for_example_search_evaluation_11e97') : undefined
                  }
                />
              </FormField>
              <FormField
                label={
                  kind === 'teams'
                    ? t('common:description_26670')
                    : t('common:business_description_aaa4b')
                }
              >
                <Textarea name="description" rows={3} maxLength={2000} />
              </FormField>
              {kind === 'teams' ? (
                <CandidatePicker
                  path="/admin/team-member-candidates"
                  selected={owners}
                  onChange={setOwners}
                  label={t('common:team_owners_f39f5')}
                  disabled={create.isPending}
                />
              ) : (
                <FormField label={t('common:project_managers_90131')}>
                  <Input
                    value={`${session.data?.user.name ?? ''} · ${session.data?.user.email ?? ''}`}
                    readOnly
                  />
                  <p className="text-xs font-normal text-muted-foreground">
                    {t('common:the_creator_is_the_initial_manager_add_other_c51f8')}
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
                {t('common:cancel_4d0b4')}
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

function CreateProjectPage({ admin }: { admin: boolean }) {
  const session = useSession()
  const actor = session.isError ? '' : (session.data?.user.id ?? '')
  return actor && session.data ? (
    <ProjectCreation key={actor} admin={admin} user={session.data.user} />
  ) : (
    <QueryState
      pending={session.isPending}
      error={session.error}
      retry={() => void session.refetch()}
    />
  )
}
function ProjectCreation({ admin, user }: { admin: boolean; user: Session['user'] }) {
  const { t: tr } = useTranslation('resources')
  const cache = useQueryClient()
  const navigate = useNavigate()
  const access = usePermissions()
  const canReplaceCreator = access.can('projects.write') && !access.isError && !access.isFetching
  const creator = { id: user.id, name: user.name, email: user.email }
  const [selected, setSelected] = useState<ResourceCandidate[]>([creator])
  const [q, setQ] = useState('')
  const [busy, setBusy] = useState(false)
  const [issue, setIssue] = useState<string | null>(null)
  const [error, setError] = useState<unknown>(null)
  const lock = useRef(false)
  const alive = useRef(true)
  useLayoutEffect(() => {
    alive.current = true
    return () => {
      alive.current = false
    }
  }, [])
  const currentActor = () =>
    alive.current && cache.getQueryData<Session>(sessionKey)?.user.id === user.id
  const candidates = useQuery({
    queryKey: ['project-creation-managers', user.id, q],
    queryFn: ({ signal }) => getProjectCreationManagers(q, signal),
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnMount: 'always',
    refetchOnWindowFocus: false,
  })
  const fresh = candidates.isSuccess && !candidates.isFetching
  const effective =
    !canReplaceCreator && !selected.some((item) => item.id === user.id)
      ? [creator, ...selected]
      : selected
  const blocked = busy || issue === 'creationUncertain'
  async function submitProject(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const session = cache.getQueryData<Session>(sessionKey)
    if (blocked || lock.current || !session || !currentActor() || !fresh) return
    if (!effective.length) {
      setIssue('creationManagerRequired')
      return
    }
    const form = new FormData(event.currentTarget)
    const name = String(form.get('name')).trim()
    const description = String(form.get('description') || '').trim()
    if (!name) {
      setIssue('blankName')
      return
    }
    lock.current = true
    setBusy(true)
    setIssue(null)
    setError(null)
    try {
      const value = await createProject(
        { name, description, manager_ids: effective.map((item) => item.id) },
        session.csrf_token,
        user.id,
        effective.map((item) => item.id),
      )
      if (currentActor()) {
        void cache.invalidateQueries({ queryKey: ['resources'] })
        navigate(`${resourcePath('projects', admin)}/${value.id}`)
      }
    } catch (failure) {
      if (currentActor()) {
        const status = isAxiosError(failure) ? failure.response?.status : undefined
        setIssue(!status || status >= 500 ? 'creationUncertain' : 'creationRejected')
        if (status && status < 500) setError(failure)
      }
    } finally {
      lock.current = false
      if (currentActor()) setBusy(false)
    }
  }
  return (
    <Page title={tr('creationTitle')} description={tr('creationHelp')}>
      <ResourceSection title={tr('creationTitle')}>
        <form
          aria-label={tr('creationTitle')}
          className="space-y-6"
          onSubmit={(event) => void submitProject(event)}
        >
          <fieldset disabled={blocked} className="space-y-6">
            <FormField label={tr('creationName')}>
              <Input name="name" autoFocus required maxLength={100} />
            </FormField>
            <FormField label={tr('businessDescription')}>
              <Textarea name="description" rows={3} maxLength={2000} />
            </FormField>
            <fieldset className="space-y-3">
              <legend className="mb-2 text-sm font-medium">{tr('managers')}</legend>
              <Input
                aria-label={tr('creationManagerSearch')}
                placeholder={tr('creationManagerSearch')}
                value={q}
                onValueChange={setQ}
              />
              <QueryState
                pending={candidates.isFetching}
                error={candidates.error}
                retry={() => void candidates.refetch()}
                empty={fresh && !candidates.data?.length}
              />
              {fresh && (
                <div className="max-h-64 space-y-2 overflow-y-auto rounded-md border p-3">
                  {candidates.data?.map((candidate) => (
                    <label key={candidate.id} className="flex items-center gap-3 text-sm">
                      <input
                        type="checkbox"
                        checked={effective.some((item) => item.id === candidate.id)}
                        disabled={!canReplaceCreator && candidate.id === user.id}
                        onChange={(event) =>
                          setSelected((old) =>
                            event.target.checked
                              ? [...old.filter((item) => item.id !== candidate.id), candidate]
                              : old.filter((item) => item.id !== candidate.id),
                          )
                        }
                      />
                      <span>
                        {candidate.name}
                        <span className="ml-2 text-muted-foreground">{candidate.email}</span>
                      </span>
                    </label>
                  ))}
                </div>
              )}
              <div className="flex flex-wrap gap-2" aria-label={tr('creationSelectedManagers')}>
                {effective.map((person) => (
                  <Button
                    key={person.id}
                    type="button"
                    size="sm"
                    variant="secondary"
                    disabled={!canReplaceCreator && person.id === user.id}
                    onClick={() =>
                      setSelected((old) => old.filter((item) => item.id !== person.id))
                    }
                    aria-label={tr('creationRemoveManager', { name: person.name })}
                  >
                    {person.name} ×
                  </Button>
                ))}
              </div>
              <p className="text-xs text-muted-foreground">
                {tr(canReplaceCreator ? 'creationManagerAdminHelp' : 'creationManagerMemberHelp')}
              </p>
            </fieldset>
            <p className="text-sm text-muted-foreground">
              {tr('newHelp', { kind: tr('project') })}
            </p>
          </fieldset>
          {issue && (
            <p role="alert" className="text-sm text-destructive">
              {tr(issue)}
            </p>
          )}
          <ErrorNotice error={error} />
          <div className="flex justify-end gap-3">
            <Link to={resourcePath('projects', admin)} className="text-sm underline">
              {tr('creationBack')}
            </Link>
            <Button type="submit" disabled={blocked || !fresh}>
              {tr('create', { kind: tr('project') })}
            </Button>
          </div>
        </form>
      </ResourceSection>
    </Page>
  )
}

import ResourceLimits from '@/views/resource-limits'
import { t, locale } from '@/i18n'
import { useTranslation } from 'react-i18next'
import { useLayoutEffect, useMemo, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Plus } from 'lucide-react'
import { createKey, listKeys, listModels, rotateKey, writeCatalog } from '@/api/catalog'
import { useSession } from '@/hooks/use-auth'
import { Page, QueryState, ErrorNotice, FormField, SaveButton } from '@/components/app/CatalogUI'
import { Dialog } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import { Table } from '@/components/ui/table'
import { Drawer } from '@/components/ui/drawer'
import { Badge } from '@/components/ui/badge'
import KeyIssueDialog from '@/views/key-delivery/owner'
import DeliveryReceiptDialog from '@/views/key-delivery/receipt'
import {
  useOwnerAuthority,
  ObsoleteOwnerOperation,
  type OwnerTicket,
} from '@/views/key-delivery/lifetime'
import { getKeyDeliveryContext } from '@/api/key-delivery'
import type { KeyDeliveryOwner } from '@/types/key-delivery'
import type { KeyDelivery, PersonalKey } from '@/types/catalog'

const statuses = {
  get pending() {
    return t('awaiting_delivery_confirmation_4b4d2')
  },
  get active() {
    return t('enabled_25d28')
  },
  get disabled() {
    return t('disabled_6c7dc')
  },
  get revoked() {
    return t('revoked_61063')
  },
}
export default function KeysPage() {
  useTranslation('common')
  const { t: deliveryText } = useTranslation('keyDeliveryOwner')

  const { data: session } = useSession()
  const cache = useQueryClient()
  const owner = useMemo<KeyDeliveryOwner>(() => ({ kind: 'personal' }), [])
  const keys = useQuery({
    queryKey: ['keys', session?.user.id],
    queryFn: async ({ signal }) => {
      const value = await listKeys()
      if (signal.aborted) throw new ObsoleteOwnerOperation()
      return value
    },
    enabled: !!session,
  })
  const models = useQuery({
    queryKey: ['models', session?.user.id],
    queryFn: async ({ signal }) => {
      const value = await listModels()
      if (signal.aborted) throw new ObsoleteOwnerOperation()
      return value
    },
    enabled: !!session,
  })
  const authority = useOwnerAuthority(owner, !!session)
  const [creating, setCreating] = useState(false)
  const [rotating, setRotating] = useState<PersonalKey | null>(null)
  const [receipt, setReceipt] = useState<{ keyId: string; requestId?: string } | null>(null)
  const [deliverySession, setDeliverySession] = useState('')
  const actorLifetime = useRef({ actor: authority.actor })
  const [statusFilter, setStatusFilter] = useState('all')
  const [viewing, setViewing] = useState<PersonalKey | null>(null)
  const [delivery, setDelivery] = useState<KeyDelivery | null>(null)
  const [saved, setSaved] = useState(false)
  const [retiring, setRetiring] = useState<PersonalKey | null>(null)
  const [notice, setNotice] = useState('')
  const [action, setAction] = useState<{ kind: 'rename' | 'revoke'; key: PersonalKey } | null>(null)
  const refresh = () => cache.invalidateQueries({ queryKey: ['keys'] })
  useLayoutEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- Session or resource renewal must destroy one-time secrets and private action state
    setDelivery(null)
    setSaved(false)
    setViewing(null)
    setAction(null)
    setRetiring(null)
    if (actorLifetime.current.actor !== authority.actor) {
      actorLifetime.current.actor = authority.actor
      setCreating(false)
      setRotating(null)
      setReceipt(null)
      setNotice('')
    }
  }, [authority.sessionVersion, authority.actor, actorLifetime])
  function admit() {
    try {
      const ticket = authority.capture()
      authority.finish(ticket)
      return true
    } catch {
      return false
    }
  }
  async function authorized<T>(run: (ticket: OwnerTicket) => Promise<T>): Promise<T> {
    const ticket = authority.capture()
    try {
      const value = await run(ticket)
      if (!authority.current(ticket)) throw new ObsoleteOwnerOperation()
      return value
    } finally {
      authority.finish(ticket)
    }
  }

  const create = useMutation({
    mutationFn: async (
      input: { name: string; model_ids: string[]; expires_at: string | null } | PersonalKey,
    ) => {
      await authorized(async (ticket) => {
        if ('id' in input) {
          const policy = await getKeyDeliveryContext(owner, ticket.controller.signal)
          if (
            !authority.current(ticket) ||
            !policy.allowed_modes.includes('manual') ||
            input.delivery_mode === 'managed_vault'
          )
            throw new ObsoleteOwnerOperation()
        }
        const result =
          'id' in input
            ? await rotateKey(input.id, ticket.csrf)
            : await createKey(input, ticket.csrf)
        if (!authority.current(ticket)) throw new ObsoleteOwnerOperation()
        setDeliverySession(authority.sessionVersion)
        setDelivery(result)
        setSaved(false)
        setCreating(false)
      })
    },
    onSuccess: refresh,
  })
  const update = useMutation({
    mutationFn: ({
      id,
      method,
      data,
    }: {
      id: string
      method: 'patch' | 'delete' | 'post'
      data?: unknown
    }) => authorized((ticket) => writeCatalog(method, `/keys/${id}`, data, ticket.csrf)),
    onSuccess: () => {
      setAction(null)
      void refresh()
    },
  })
  const retirement = useMutation({
    mutationFn: (replacementID: string) =>
      authorized((ticket) =>
        writeCatalog(
          'post',
          `/keys/${retiring!.id}/complete-rotation`,
          { replacement_key_id: replacementID },
          ticket.csrf,
        ),
      ),
    onSuccess: () => {
      setRetiring(null)
      setNotice('rotation_complete_the_original_key_has_been_revoked_e319c')
      void refresh()
    },
  })
  const finish = useMutation({
    mutationFn: (confirm: boolean) =>
      authorized((ticket) =>
        writeCatalog(
          confirm ? 'post' : 'delete',
          `/keys/${delivery!.key.id}${confirm ? '/confirm' : ''}`,
          confirm ? {} : undefined,
          ticket.csrf,
        ),
      ),
    onSuccess: (_, confirmed) => {
      if (confirmed && delivery?.key.replaces_key_id)
        setNotice('replacement_delivery_confirmed_the_original_key_remains_usable_a698e')
      setDelivery(null)
      setSaved(false)
      void refresh()
    },
  })
  return (
    <Page
      title={t('my_api_keys_9df22')}
      description={t('keys_authorize_your_personal_calls_secrets_appear_once_a36c1')}
    >
      <p className="rounded-lg border bg-muted/30 px-4 py-3 text-sm">
        {t('the_full_api_key_appears_only_once_after_d851d')}
      </p>
      {notice && (
        <p role="status" className="text-sm">
          {t(notice)}
        </p>
      )}
      <ErrorNotice error={!creating && !delivery ? create.error || update.error : null} />
      <QueryState
        pending={(!authority.fresh && !keys.error && !models.error) || keys.isPending}
        error={keys.error || models.error}
        retry={() => {
          void keys.refetch()
          void models.refetch()
        }}
        empty={keys.data?.length === 0}
      />
      <div className="flex flex-wrap items-center justify-between gap-4">
        <div className="flex items-center gap-2" aria-label={t('key_status_filter_f5885')}>
          {[
            { id: 'active', label: t('active_f78d0') },
            { id: 'all', label: t('all_778fc') },
            { id: 'disabled', label: t('disabled_6c7dc') },
          ].map((item) => (
            <Button
              key={item.id}
              size="sm"
              variant={statusFilter === item.id ? 'secondary' : 'ghost'}
              aria-pressed={statusFilter === item.id}
              onClick={() => setStatusFilter(item.id)}
            >
              {item.label}
            </Button>
          ))}
        </div>
        <Button
          disabled={!authority.fresh}
          onClick={() => {
            if (!admit()) return
            create.reset()
            setRotating(null)
            setCreating(true)
          }}
        >
          <Plus className="size-4" />
          {t('create_key_bd80d')}
        </Button>
      </div>
      <Table aria-label={t('api_key_list_9dd98')}>
        <thead>
          <tr>
            <th>{t('name_1be7a')}</th>
            <th>API Key</th>
            <th>{t('delivery_method_838f9')}</th>
            <th>{t('model_scope_24575')}</th>
            <th>{t('status_62e95')}</th>
            <th>{t('expires_22246')}</th>
            <th>{t('actions_f3ea6')}</th>
          </tr>
        </thead>
        <tbody>
          {(authority.fresh ? keys.data : [])
            ?.filter((key) => statusFilter === 'all' || key.status === statusFilter)
            .map((key) => (
              <tr key={key.id}>
                <td>
                  {key.name}
                  {key.replaces_key_id && (
                    <p className="mt-1 text-xs text-muted-foreground">
                      {t('replaces_fe40b')}
                      {keys.data?.find((source) => source.id === key.replaces_key_id)?.name ??
                        key.replaces_key_id}{' '}
                      ·{' '}
                      {keys.data?.find((source) => source.id === key.replaces_key_id)?.status ===
                      'revoked'
                        ? t('original_key_revoked_818b5')
                        : t('original_key_not_revoked_2837f')}
                    </p>
                  )}
                </td>
                <td className="font-mono">{key.prefix}…</td>
                <td>
                  <Badge variant="outline">
                    {deliveryText(key.delivery_mode ?? 'unknownMode')}
                  </Badge>
                </td>
                <td>
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => {
                      if (admit()) setViewing(key)
                    }}
                  >
                    {t('modelCount', { count: key.model_ids.length })}
                  </Button>
                </td>
                <td>
                  <Badge variant="outline">{statuses[key.status]}</Badge>
                </td>
                <td className="whitespace-nowrap">
                  {key.expires_at
                    ? new Date(key.expires_at).toLocaleString(locale())
                    : t('no_expiration_d0b37')}
                </td>
                <td>
                  <div className="flex gap-1">
                    {key.delivery_mode === 'managed_vault' && (
                      <Button
                        size="sm"
                        variant="ghost"
                        onClick={() => {
                          if (admit()) setReceipt({ keyId: key.id })
                        }}
                      >
                        {deliveryText('receiptTitle')}
                      </Button>
                    )}
                    <Button
                      size="sm"
                      variant="ghost"
                      onClick={() => {
                        if (admit()) setViewing(key)
                      }}
                    >
                      {t('limits:title')}
                    </Button>
                    {(key.status === 'active' || key.status === 'disabled') && (
                      <>
                        <Button
                          size="sm"
                          variant="ghost"
                          disabled={update.isPending}
                          onClick={() => {
                            if (!admit()) return
                            update.reset()
                            setAction({ kind: 'rename', key })
                          }}
                        >
                          {t('rename_1cd80')}
                        </Button>
                        <Button
                          size="sm"
                          variant="ghost"
                          disabled={update.isPending}
                          onClick={() => {
                            if (!admit()) return
                            update.mutate({
                              id: key.id,
                              method: 'patch',
                              data: { enabled: key.status !== 'active' },
                            })
                          }}
                        >
                          {key.status === 'active' ? t('disable_d989e') : t('enable_d4e9c')}
                        </Button>
                        <Button
                          size="sm"
                          variant="ghost"
                          disabled={create.isPending}
                          onClick={() => {
                            if (!admit()) return
                            create.reset()
                            finish.reset()
                            if (key.delivery_mode === 'managed_vault') {
                              setRotating(key)
                              setCreating(true)
                            } else create.mutate(key)
                          }}
                        >
                          {t('rotate_d822d')}
                        </Button>
                        {keys.data?.some(
                          (candidate) =>
                            candidate.replaces_key_id === key.id && candidate.status === 'active',
                        ) && (
                          <Button
                            size="sm"
                            variant="ghost"
                            onClick={() => {
                              if (!admit()) return
                              retirement.reset()
                              setRetiring(key)
                            }}
                          >
                            {t('complete_rotation_70fcd')}
                          </Button>
                        )}
                      </>
                    )}
                    {key.status !== 'revoked' && (
                      <Button
                        size="sm"
                        variant="ghost"
                        onClick={() => {
                          if (!admit()) return
                          update.reset()
                          setAction({ kind: 'revoke', key })
                        }}
                      >
                        {t('revoke_9fcef')}
                      </Button>
                    )}
                  </div>
                </td>
              </tr>
            ))}
        </tbody>
      </Table>
      <Drawer
        open={authority.fresh && !!viewing}
        onOpenChange={(open) => {
          if (!open) setViewing(null)
        }}
        title={t('limits:keyDetails', { name: viewing?.name ?? '' })}
        width={720}
      >
        <Table>
          <thead>
            <tr>
              <th>{t('models_98fd0')}</th>
              <th>{t('protocol_993df')}</th>
              <th>{t('status_62e95')}</th>
            </tr>
          </thead>
          <tbody>
            {viewing?.model_ids.map((id) => {
              const model = models.data?.find((m) => m.id === id)
              return (
                <tr key={id}>
                  <td>{model?.name ?? id}</td>
                  <td>OpenAI Chat</td>
                  <td>{model ? t('granted_284cc') : t('grant_no_longer_valid_80b35')}</td>
                </tr>
              )
            })}
          </tbody>
        </Table>
        {viewing && (
          <div className="mt-6">
            <ResourceLimits
              key={viewing.id}
              path={`/keys/${viewing.id}`}
              child
              canEdit={viewing.status === 'active' || viewing.status === 'disabled'}
            />
          </div>
        )}
      </Drawer>
      {creating && (
        <KeyIssueDialog
          key={authority.actor + (rotating?.id ?? 'new')}
          owner={owner}
          authority={authority}
          models={(models.data ?? []).filter((m) => m.status === 'active')}
          predecessor={rotating ?? undefined}
          title={rotating ? deliveryText('rotate') : t('create_personal_key_80e1b')}
          description={t('choose_the_allowed_models_confirm_delivery_within_10_1b1ae')}
          manualLabel={t('create_and_reveal_key_5bf84')}
          onManual={(input) => create.mutateAsync(rotating ?? input)}
          onReceipt={(value) => {
            setCreating(false)
            setRotating(null)
            setReceipt({ keyId: value.key_id, requestId: value.request_id })
            void refresh()
          }}
          onClose={() => {
            setCreating(false)
            setRotating(null)
          }}
        />
      )}
      {receipt && (
        <DeliveryReceiptDialog
          key={authority.actor + receipt.keyId}
          owner={owner}
          authority={authority}
          keyId={receipt.keyId}
          requestId={receipt.requestId}
          onClose={() => setReceipt(null)}
          onChanged={() => {
            void refresh()
          }}
        />
      )}
      <Dialog
        width={560}
        open={authority.fresh && deliverySession === authority.sessionVersion && !!delivery}
        onOpenChange={(open) => {
          if (!open && !finish.isPending && admit()) finish.mutate(false)
        }}
        busy={finish.isPending}
        title={t('save_your_key_77b9f')}
        description={t('this_secret_appears_once_confirm_that_you_saved_131d0')}
      >
        {delivery && (
          <div className="space-y-5">
            <code
              className="block break-all rounded-md border bg-muted p-4 text-sm"
              aria-label={t('one_time_secret_8b246')}
            >
              {delivery.secret}
            </code>
            <p className="text-xs text-muted-foreground">
              {t('store_it_in_your_secret_manager_do_not_0e9d8')}
            </p>
            <label className="flex items-center gap-2 text-sm">
              <input type="checkbox" checked={saved} onChange={(e) => setSaved(e.target.checked)} />
              {t('i_have_stored_this_key_securely_cc546')}
            </label>
            {delivery.key.replaces_key_id && (
              <p className="text-sm text-muted-foreground">
                {t('delivery_confirmation_does_not_verify_a_call_a_4ce62')}
              </p>
            )}
            <ErrorNotice error={finish.error} />
            <div className="flex gap-2">
              <Button
                disabled={!saved || finish.isPending}
                onClick={() => {
                  if (admit()) finish.mutate(true)
                }}
              >
                {delivery.key.replaces_key_id
                  ? t('confirm_delivery_615f4')
                  : t('confirm_and_enable_d4f31')}
              </Button>
              <Button
                variant="outline"
                disabled={finish.isPending}
                onClick={() => {
                  if (admit()) finish.mutate(false)
                }}
              >
                {t('cancel_and_revoke_176bb')}
              </Button>
            </div>
          </div>
        )}
      </Dialog>
      <Dialog
        open={authority.fresh && !!retiring}
        onOpenChange={(open) => {
          if (!open) setRetiring(null)
        }}
        busy={retirement.isPending}
        title={t('complete_key_rotation_e8920')}
        description={t('switch_your_application_to_the_replacement_and_make_71853')}
      >
        <form
          className="space-y-5"
          aria-label={t('complete_key_rotation_e8920')}
          onSubmit={(event) => {
            event.preventDefault()
            if (!admit()) return
            if (!retiring || retirement.isPending) return
            retirement.mutate(String(new FormData(event.currentTarget).get('replacement')))
          }}
        >
          <FormField label={t('replacement_key_b3c07')}>
            <select
              name="replacement"
              required
              className="h-9 w-full rounded-md border bg-background px-3 text-sm"
              disabled={retirement.isPending}
            >
              {keys.data
                ?.filter((key) => key.replaces_key_id === retiring?.id && key.status === 'active')
                .map((key) => (
                  <option key={key.id} value={key.id}>
                    {key.name} · {key.prefix}…
                  </option>
                ))}
            </select>
          </FormField>
          <p className="text-sm text-muted-foreground">
            {t('original_key_bef53')}
            {retiring?.name} · {retiring?.prefix}
            {t('if_a_successful_call_has_not_been_recorded_df32d')}
          </p>
          <ErrorNotice error={retirement.error} />
          <SaveButton pending={retirement.isPending}>
            {t('verify_call_and_revoke_original_ab13d')}
          </SaveButton>
        </form>
      </Dialog>
      <Dialog
        open={authority.fresh && !!action}
        onOpenChange={(open) => {
          if (!open) setAction(null)
        }}
        busy={update.isPending}
        title={action?.kind === 'rename' ? t('rename_key_91684') : t('revoke_key_a9725')}
        description={
          action?.kind === 'rename'
            ? t('the_name_identifies_this_key_it_does_not_a2d86')
            : t('revocation_immediately_rejects_new_requests_and_cannot_be_819e5')
        }
      >
        <form
          className="space-y-5"
          onSubmit={(e) => {
            e.preventDefault()
            if (!admit()) return
            if (!action || update.isPending) return
            update.mutate({
              id: action.key.id,
              method: action.kind === 'rename' ? 'patch' : 'delete',
              data:
                action.kind === 'rename'
                  ? { name: String(new FormData(e.currentTarget).get('name')).trim() }
                  : undefined,
            })
          }}
        >
          {action?.kind === 'rename' && (
            <FormField label={t('name_1be7a')}>
              <Input name="name" defaultValue={action.key.name} required maxLength={100} />
            </FormField>
          )}
          <ErrorNotice error={update.error} />
          <SaveButton pending={update.isPending}>
            {action?.kind === 'rename' ? t('save_fadf2') : t('confirm_revocation_f7e81')}
          </SaveButton>
        </form>
      </Dialog>
    </Page>
  )
}

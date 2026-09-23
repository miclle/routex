import { t } from '@/i18n'
import { useTranslation } from 'react-i18next'
import { LanguageSwitcher } from './LanguageSwitcher'
import { useEffect, useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import {
  Route,
  LogOut,
  PanelLeftClose,
  Menu as MenuIcon,
  ChevronsUpDown,
  LayoutDashboard,
  KeyRound,
  Bot,
  PlayCircle,
  History,
  UserRound,
  ShieldCheck,
  Settings,
  ArrowLeft,
  ChevronRight,
  Cloud,
} from 'lucide-react'
import { NavLink, Outlet, useLocation, useNavigate } from 'react-router'
import { authError, logout } from '@/api/auth'
import { useSession, sessionKey, setupKey } from '@/hooks/use-auth'
import { usePermissions } from '@/hooks/use-permissions'
import { Button } from '@/components/ui/button'
import { Drawer } from '@/components/ui/drawer'
import { Menu, MenuItem } from '@/components/ui/menu'

const memberNav = [
  {
    to: '/',
    get label() {
      return t('overview_50604')
    },
    icon: LayoutDashboard,
  },
  { to: '/keys', label: 'API Keys', icon: KeyRound },
  {
    to: '/models',
    get label() {
      return t('model_catalog_a71ae')
    },
    icon: Bot,
  },
  { to: '/playground', label: 'Playground', icon: PlayCircle },
  {
    to: '/calls',
    get label() {
      return t('call_history_16e7d')
    },
    icon: History,
  },
]
const accountNav = [
  {
    to: '/account',
    get label() {
      return t('profile_d562e')
    },
    icon: UserRound,
  },
  {
    to: '/account/security',
    get label() {
      return t('security_8bf43')
    },
    icon: ShieldCheck,
  },
]
const adminNav = [
  {
    to: '/admin/members',
    get label() {
      return t('members_c1ee9')
    },
    icon: UserRound,
    permission: 'members.read',
    get group() {
      return t('members_and_access_34488')
    },
  },
  {
    to: '/admin/roles',
    get label() {
      return t('roles_and_permissions_0c1a0')
    },
    icon: ShieldCheck,
    permission: 'roles.read',
    get group() {
      return t('members_and_access_34488')
    },
  },
  {
    to: '/admin/models',
    get label() {
      return t('models_98fd0')
    },
    icon: Bot,
    permission: 'models.read_all',
    get group() {
      return t('service_access_3fa7c')
    },
  },
  {
    to: '/admin/providers',
    get label() {
      return t('providers_703c9')
    },
    icon: Cloud,
    permission: 'providers.read',
    get group() {
      return t('service_access_3fa7c')
    },
  },
  {
    to: '/admin/calls',
    get label() {
      return t('platform_calls_4c73e')
    },
    icon: History,
    permission: 'calls.read_all',
    get group() {
      return t('operations_8e37c')
    },
  },
  {
    to: '/admin/auth',
    get label() {
      return t('authentication_0ff9a')
    },
    icon: Settings,
    permission: 'registration.write',
    get group() {
      return t('system_administration_04ca1')
    },
  },
]

export default function AppShell() {
  useTranslation()

  const session = useSession()
  const access = usePermissions()
  const adminItems = adminNav.filter((item) => access.can(item.permission))
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const { pathname } = useLocation()
  const admin = pathname.startsWith('/admin/')
  const [collapsed, setCollapsed] = useState(false)
  const [mobileOpen, setMobileOpen] = useState(false)
  const [desktop, setDesktop] = useState(() => window.innerWidth >= 992)
  useEffect(() => {
    const update = () => setDesktop(window.innerWidth >= 992)
    window.addEventListener('resize', update)
    return () => window.removeEventListener('resize', update)
  }, [])
  const mutation = useMutation({
    mutationFn: () => logout(session.data!.csrf_token),
    onSuccess: () => {
      queryClient.clear()
      queryClient.setQueryData(setupKey, { initialized: true })
      queryClient.setQueryData(sessionKey, null)
      navigate('/login', { replace: true })
    },
  })
  const title =
    [...accountNav, ...memberNav, ...adminNav].find((item) => item.to === pathname)?.label ??
    (pathname === '/admin/models/new'
      ? t('add_model_532a6')
      : pathname.startsWith('/admin/providers/')
        ? t('provider_details_50ab1')
        : pathname.startsWith('/admin/models/')
          ? t('model_details_84b34')
          : pathname.startsWith('/admin/members/')
            ? t('member_details_e20da')
            : 'RouteX')
  const compact = desktop && collapsed
  function links(items: typeof memberNav) {
    return items.map((item) => (
      <NavLink
        key={item.to}
        to={item.to}
        end={item.to === '/' || item.to === '/account'}
        title={compact ? item.label : undefined}
        onClick={() => setMobileOpen(false)}
        className={({ isActive }) =>
          `mx-1 my-1 flex h-10 items-center gap-3 rounded-md px-5 text-sm ${compact ? 'justify-center px-0' : ''} ${isActive ? 'bg-accent font-medium text-primary' : 'text-foreground hover:bg-muted'}`
        }
      >
        <item.icon className="size-4 shrink-0" aria-hidden="true" />
        {!compact && <span>{item.label}</span>}
      </NavLink>
    ))
  }
  function sidebar() {
    return (
      <div className="flex h-full flex-col">
        <div className="flex h-20 shrink-0 items-center justify-between gap-2 p-5">
          <div className="flex items-center gap-2">
            <button
              aria-label={compact ? t('expand_sidebar_8ab51') : 'RouteX'}
              onClick={() => compact && setCollapsed(false)}
              className="flex size-8 shrink-0 items-center justify-center rounded-md bg-primary text-primary-foreground"
            >
              <Route className="size-5" />
            </button>
            {!compact && (
              <>
                <span className="text-xl font-semibold">RouteX</span>
                {admin && (
                  <span className="whitespace-nowrap text-xs text-muted-foreground">
                    {t('administration_3504c')}
                  </span>
                )}
              </>
            )}
          </div>
          {desktop && !compact && (
            <Button
              variant="ghost"
              size="icon"
              aria-label={t('collapse_sidebar_c114f')}
              onClick={() => setCollapsed(true)}
            >
              <PanelLeftClose className="size-4" />
            </Button>
          )}
        </div>
        <div className="min-h-0 flex-1 overflow-auto">
          <nav aria-label={t('main_navigation_fb1c7')}>
            {admin ? (
              <>
                {links([{ to: '/', label: t('back_to_workspace_99a20'), icon: ArrowLeft }])}
                {[
                  t('operations_8e37c'),
                  t('members_and_access_34488'),
                  t('service_access_3fa7c'),
                  t('system_administration_04ca1'),
                ]
                  .filter((group) => adminItems.some((item) => item.group === group))
                  .map((group) => (
                    <div key={group}>
                      {!compact && (
                        <p className="px-6 pt-5 pb-2 text-xs text-muted-foreground">{group}</p>
                      )}
                      {links(adminItems.filter((item) => item.group === group))}
                    </div>
                  ))}
              </>
            ) : (
              links(memberNav)
            )}
          </nav>
          {!admin && (
            <>
              <hr className="mx-4 my-2" />
              <nav aria-label={t('account_navigation_5ef98')}>
                {links(accountNav)}
                {adminItems.length > 0 && (
                  <NavLink
                    to={adminItems[0].to}
                    onClick={() => setMobileOpen(false)}
                    className="mx-1 flex h-10 items-center gap-3 rounded-md px-5 text-sm hover:bg-muted"
                  >
                    <Settings className="size-4 shrink-0" />
                    {!compact && (
                      <>
                        <span className="flex-1">{t('platform_administration_678b6')}</span>
                        <ChevronRight className="size-3" />
                      </>
                    )}
                  </NavLink>
                )}
              </nav>
            </>
          )}
        </div>
        <div className="border-t p-3">
          <Menu
            label={t('value_current_account_77ae5', { v0: session.data?.user.name })}
            trigger={
              <>
                <span className="flex size-7 shrink-0 items-center justify-center rounded-full bg-muted text-xs">
                  {session.data?.user.name.slice(0, 2).toUpperCase()}
                </span>
                {!compact && (
                  <>
                    <span className="min-w-0 flex-1 truncate">{session.data?.user.name}</span>
                    <ChevronsUpDown className="size-3" />
                  </>
                )}
              </>
            }
          >
            <div className="border-b px-3 py-2 text-sm">
              <p className="font-medium">{session.data?.user.name}</p>
              <p className="text-muted-foreground">{session.data?.user.email}</p>
            </div>
            <MenuItem onClick={() => navigate('/account')}>
              <UserRound className="size-4" />
              {t('profile_d562e')}
            </MenuItem>
            <MenuItem onClick={() => navigate('/account/security')}>
              <ShieldCheck className="size-4" />
              {t('security_8bf43')}
            </MenuItem>
            {admin && (
              <MenuItem onClick={() => navigate('/')}>
                <ArrowLeft className="size-4" />
                {t('back_to_workspace_99a20')}
              </MenuItem>
            )}
            <MenuItem disabled={mutation.isPending} onClick={() => mutation.mutate()}>
              <LogOut className="size-4" />
              {mutation.isPending ? t('signing_out_15f2a') : t('sign_out_09477')}
            </MenuItem>
          </Menu>
        </div>
      </div>
    )
  }
  return (
    <div className="flex h-dvh overflow-hidden bg-background text-foreground">
      {desktop && (
        <aside style={{ width: compact ? 80 : 248 }} className="h-full shrink-0 border-r">
          {sidebar()}
        </aside>
      )}
      <Drawer
        open={mobileOpen}
        onOpenChange={setMobileOpen}
        title={t('navigation_c7ed2')}
        side="left"
        width={280}
        bodyClassName="p-0"
      >
        {sidebar()}
      </Drawer>
      <div className="flex min-w-0 flex-1 flex-col">
        <header
          aria-label={t('page_navigation_0d2f9')}
          className="flex h-16 shrink-0 items-center gap-2 border-b px-4 min-[992px]:px-6"
        >
          {!desktop && (
            <Button
              variant="ghost"
              size="icon"
              aria-label={t('open_sidebar_ef976')}
              onClick={() => setMobileOpen(true)}
            >
              <MenuIcon className="size-4" />
            </Button>
          )}
          <div className="flex min-w-0 items-center gap-3">
            {(pathname.startsWith('/admin/providers/') ||
              pathname.startsWith('/admin/models/')) && (
              <>
                <NavLink
                  to={
                    pathname.startsWith('/admin/providers/') ? '/admin/providers' : '/admin/models'
                  }
                  className="text-base"
                >
                  {pathname.startsWith('/admin/providers/')
                    ? t('providers_703c9')
                    : t('models_98fd0')}
                </NavLink>
                <ChevronRight className="size-4 text-muted-foreground" />
              </>
            )}
            <h1 className="truncate text-base font-semibold">{title}</h1>
          </div>
          <div className="ml-auto">
            <LanguageSwitcher />
          </div>
        </header>
        <main className="min-h-0 flex-1 overflow-auto p-4 min-[992px]:p-6">
          {mutation.isError && (
            <p role="alert" className="mb-4 text-sm text-destructive">
              {t('sign_out_failed_f4391')}
              {authError(mutation.error)}
            </p>
          )}
          <Outlet />
        </main>
      </div>
    </div>
  )
}

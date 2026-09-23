import { useState, type FormEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useLocation, useNavigate } from 'react-router'
import axios from 'axios'
import { ArrowRight, LoaderCircle, Route } from 'lucide-react'
import { getRegistration, register } from '@/api/governance'
import { authError, login, setup } from '@/api/auth'
import { sessionKey, setupKey } from '@/hooks/use-auth'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import type { SetupInput } from '@/types/auth'

export default function AuthPage({ mode }: { mode: 'login' | 'setup' | 'register' }) {
  const isSetup = mode === 'setup'
  const isRegister = mode === 'register'
  const registration = useQuery({ queryKey: ['auth', 'registration'], queryFn: () => getRegistration(), enabled: !isSetup, retry: false })
  const navigate = useNavigate()
  const location = useLocation()
  const queryClient = useQueryClient()
  const [validation, setValidation] = useState('')
  const mutation = useMutation({
    mutationFn: (input: SetupInput) => isSetup ? setup(input) : isRegister ? register(input) : login({ email: input.email, password: input.password }),
    onSuccess: (session) => {
      queryClient.clear()
      queryClient.setQueryData(setupKey, { initialized: true })
      queryClient.setQueryData(sessionKey, session)
      navigate('/', { replace: true })
    },
    onError: (error) => {
      if (isSetup && axios.isAxiosError(error) && error.response?.status === 409) {
        queryClient.setQueryData(setupKey, { initialized: true })
        navigate('/login', { replace: true, state: { message: authError(error) } })
      }
    },
  })

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (mutation.isPending) return
    const data = new FormData(event.currentTarget)
    const password = String(data.get('password') ?? '')
    const name = String(data.get('name') ?? '').trim()
    if (isSetup || isRegister) {
      const bytes = new TextEncoder().encode(password).length
      if (bytes < 12 || bytes > 72) { setValidation('密码长度须为 12–72 个 UTF-8 字节；中文等字符可能占多个字节。'); return }
      if (isSetup && password !== data.get('confirmPassword')) { setValidation('两次输入的密码不一致。'); return }
      if (!name) { setValidation(isSetup ? '请输入管理员姓名。' : '请输入姓名。'); return }
    }
    setValidation('')
    mutation.mutate({ email: String(data.get('email') ?? '').trim(), password, name })
  }

  const notice = typeof location.state?.message === 'string' ? location.state.message : ''
  const error = validation || (mutation.isError ? isRegister && axios.isAxiosError(mutation.error) && mutation.error.response?.status === 409 ? '此邮箱已注册，请登录或使用其他邮箱。' : isRegister && axios.isAxiosError(mutation.error) && mutation.error.response?.status === 403 ? '注册已关闭，请联系管理员。' : authError(mutation.error) : '')
  if (isRegister && (!registration.data?.enabled || registration.isPending)) return <main className="flex min-h-screen items-center justify-center p-6"><section className="w-full max-w-[500px] space-y-6 rounded-lg border p-6 text-center"><h1 className="text-2xl font-semibold">创建 RouteX 账户</h1><p role={registration.isError ? 'alert' : 'status'}>{registration.isPending ? '正在确认注册设置…' : registration.isError ? '无法读取注册设置，请稍后重试。' : '当前未开放注册，请联系管理员。'}</p>{registration.isError && <Button onClick={() => void registration.refetch()}>重试</Button>}<Link to="/login" className="block underline">返回登录</Link></section></main>

  return (
    <main className="flex min-h-screen items-center justify-center bg-background p-6">
      <section className="w-full max-w-[500px] space-y-6">
        <div aria-label="RouteX 品牌" className="flex justify-center"><span className="flex size-8 items-center justify-center rounded-md bg-primary text-primary-foreground"><Route className="size-5" aria-hidden="true" /></span></div>
        <div className="space-y-6 rounded-lg border p-6">
          <header className="space-y-3 text-center">
            <h1 className="text-[30px] font-semibold leading-[38px]">{isSetup ? '初始化 RouteX' : isRegister ? '加入 RouteX' : '登录模型服务控制台'}</h1>
            {isSetup && <p className="text-sm leading-6 text-muted-foreground">创建首个管理员账户。完成后将自动登录。</p>}
          </header>
          {notice && <p role="status" className="rounded-md border bg-muted/40 p-3 text-sm leading-6">{notice}</p>}
          <form aria-label={isSetup ? '创建管理员' : isRegister ? '注册' : '登录'} onSubmit={submit} className="space-y-5">
            <fieldset disabled={mutation.isPending} className="space-y-5">
              {(isSetup || isRegister) && <div className="space-y-2"><label htmlFor="name" className="text-sm font-medium">{isSetup ? '管理员姓名' : '姓名'}</label><Input id="name" name="name" autoComplete="name" required maxLength={100} /></div>}
              <div className="space-y-2"><label htmlFor="email" className="text-sm font-medium">工作邮箱</label><Input id="email" name="email" type="email" autoComplete="username" placeholder="you@company.com" required maxLength={254} /></div>
              <div className="space-y-2"><label htmlFor="password" className="text-sm font-medium">密码</label><Input id="password" name="password" type="password" autoComplete={isSetup || isRegister ? 'new-password' : 'current-password'} required aria-describedby={isSetup || isRegister ? 'password-hint' : undefined} />{(isSetup || isRegister) && <p id="password-hint" className="text-xs leading-5 text-muted-foreground">使用 12–72 个 UTF-8 字节的密码，建议使用较长且独有的密码短语。</p>}</div>
              {isSetup && <div className="space-y-2"><label htmlFor="confirmPassword" className="text-sm font-medium">确认密码</label><Input id="confirmPassword" name="confirmPassword" type="password" autoComplete="new-password" required /></div>}
            </fieldset>
            {error && <p role="alert" className="rounded-md border border-destructive/30 bg-destructive/5 p-3 text-sm leading-6 text-destructive">{error}</p>}
            <Button type="submit" size="lg" disabled={mutation.isPending} className="w-full">
              {mutation.isPending ? <LoaderCircle className="size-4 animate-spin" aria-hidden="true" /> : null}
              {mutation.isPending ? '正在提交…' : isSetup ? '创建管理员并进入' : isRegister ? '提交注册' : '登录'}
              {!mutation.isPending && <ArrowRight className="size-4" aria-hidden="true" />}
            </Button>
          </form>
        </div>
          <p className="text-center text-sm leading-5 text-muted-foreground">{isSetup ? '每个站点仅能初始化一次，请妥善保存管理员账户。' : isRegister ? <Link to="/login">已有账户？去登录</Link> : registration.data?.enabled ? <Link to="/register">还没有账户？创建账户</Link> : '没有账户？请联系你所在企业的管理员。'}</p>
      </section>
    </main>
  )
}

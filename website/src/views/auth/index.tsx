import { useState, type FormEvent } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useLocation, useNavigate } from 'react-router'
import axios from 'axios'
import { ArrowRight, LoaderCircle, Route, ShieldCheck } from 'lucide-react'
import { authError, login, setup } from '@/api/auth'
import { sessionKey, setupKey } from '@/hooks/use-auth'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import type { SetupInput } from '@/types/auth'

export default function AuthPage({ mode }: { mode: 'login' | 'setup' }) {
  const isSetup = mode === 'setup'
  const navigate = useNavigate()
  const location = useLocation()
  const queryClient = useQueryClient()
  const [validation, setValidation] = useState('')
  const mutation = useMutation({
    mutationFn: (input: SetupInput) => isSetup ? setup(input) : login({ email: input.email, password: input.password }),
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
    if (isSetup) {
      const bytes = new TextEncoder().encode(password).length
      if (bytes < 12 || bytes > 72) { setValidation('密码长度须为 12–72 个 UTF-8 字节；中文等字符可能占多个字节。'); return }
      if (password !== data.get('confirmPassword')) { setValidation('两次输入的密码不一致。'); return }
      if (!name) { setValidation('请输入管理员姓名。'); return }
    }
    setValidation('')
    mutation.mutate({ email: String(data.get('email') ?? '').trim(), password, name })
  }

  const notice = typeof location.state?.message === 'string' ? location.state.message : ''
  const error = validation || (mutation.isError ? authError(mutation.error) : '')

  return (
    <main className="grid min-h-screen bg-background lg:grid-cols-[0.9fr_1.1fr]">
      <aside className="relative hidden flex-col justify-between border-r bg-muted/40 p-12 lg:flex xl:p-16">
        <div className="flex items-center gap-3 text-xl font-semibold"><Route className="size-7" aria-hidden="true" />RouteX</div>
        <div className="max-w-md space-y-6">
          <div className="h-px w-12 bg-foreground" />
          <p className="text-xs font-medium tracking-[0.2em] text-muted-foreground">企业 AI 控制面</p>
          <h1 className="text-4xl leading-snug font-semibold tracking-tight">让每一次 AI 调用，<br />有据可循。</h1>
          <p className="max-w-sm text-base leading-7 text-muted-foreground">从一个可信的身份开始，建立企业的 AI 访问边界。</p>
        </div>
        <p className="flex items-center gap-2 text-xs text-muted-foreground"><ShieldCheck className="size-4" aria-hidden="true" />企业自主管理 · 统一访问入口</p>
      </aside>
      <section className="flex items-center justify-center px-6 py-12 sm:px-12">
        <div className="w-full max-w-sm space-y-8">
          <div className="flex items-center gap-2 text-lg font-semibold lg:hidden"><Route aria-hidden="true" />RouteX</div>
          <header className="space-y-3">
            <p className="text-xs font-medium tracking-widest text-muted-foreground">{isSetup ? '首次使用' : '欢迎回来'}</p>
            <h2 className="text-2xl font-semibold tracking-tight">{isSetup ? '初始化 RouteX' : '登录控制台'}</h2>
            <p className="text-sm leading-6 text-muted-foreground">{isSetup ? '创建首个管理员账户。完成后将自动登录。' : '使用你的账户访问 RouteX。'}</p>
          </header>
          {notice && <p role="status" className="rounded-md border bg-muted/40 p-3 text-sm leading-6">{notice}</p>}
          <form aria-label={isSetup ? '创建管理员' : '登录'} onSubmit={submit} className="space-y-5">
            <fieldset disabled={mutation.isPending} className="space-y-5">
              {isSetup && <div className="space-y-2"><label htmlFor="name" className="text-sm font-medium">管理员姓名</label><Input id="name" name="name" autoComplete="name" required maxLength={100} /></div>}
              <div className="space-y-2"><label htmlFor="email" className="text-sm font-medium">工作邮箱</label><Input id="email" name="email" type="email" autoComplete="username" placeholder="you@company.com" required maxLength={254} /></div>
              <div className="space-y-2"><label htmlFor="password" className="text-sm font-medium">密码</label><Input id="password" name="password" type="password" autoComplete={isSetup ? 'new-password' : 'current-password'} required aria-describedby={isSetup ? 'password-hint' : undefined} />{isSetup && <p id="password-hint" className="text-xs leading-5 text-muted-foreground">使用 12–72 个 UTF-8 字节的密码，建议使用较长且独有的密码短语。</p>}</div>
              {isSetup && <div className="space-y-2"><label htmlFor="confirmPassword" className="text-sm font-medium">确认密码</label><Input id="confirmPassword" name="confirmPassword" type="password" autoComplete="new-password" required /></div>}
            </fieldset>
            {error && <p role="alert" className="rounded-md border border-destructive/30 bg-destructive/5 p-3 text-sm leading-6 text-destructive">{error}</p>}
            <Button type="submit" size="lg" disabled={mutation.isPending} className="w-full">
              {mutation.isPending ? <LoaderCircle className="size-4 animate-spin" aria-hidden="true" /> : null}
              {mutation.isPending ? '正在提交…' : isSetup ? '创建管理员并进入' : '登录'}
              {!mutation.isPending && <ArrowRight className="size-4" aria-hidden="true" />}
            </Button>
          </form>
          <p className="border-t pt-5 text-xs leading-5 text-muted-foreground">{isSetup ? '每个站点仅能初始化一次，请妥善保存管理员账户。' : '没有账户？请联系你所在企业的管理员。'}</p>
        </div>
      </section>
    </main>
  )
}

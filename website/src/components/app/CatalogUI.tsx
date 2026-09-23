import type { ReactNode } from 'react'
import { useSession } from '@/hooks/use-auth'
import { Button } from '@/components/ui/button'
import { catalogError } from '@/api/catalog'

export function AdminOnly({ children }: { children: ReactNode }) {
  const { data } = useSession()
  return data?.user.role === 'admin' ? children : <Page title="无权访问" description="此页面仅向平台管理员开放。" />
}
export function Page({ title, description, action, children }: { title: string; description: string; action?: ReactNode; children?: ReactNode }) {
  return <section className="space-y-6"><header className={action ? "flex justify-end" : "sr-only"}><div className="sr-only"><h1>{title}</h1><p>{description}</p></div>{action}</header>{children}</section>
}
export function QueryState({ pending, error, retry, empty }: { pending: boolean; error: unknown; retry: () => void; empty?: boolean }) {
  if (pending) return <p role="status" className="py-8 text-sm text-muted-foreground">正在加载…</p>
  if (error) return <div className="space-y-3 rounded-lg border p-5"><ErrorNotice error={error} /><Button variant="outline" onClick={retry}>重试</Button></div>
  return empty ? <p className="rounded-lg border border-dashed p-8 text-center text-sm text-muted-foreground">暂无数据。</p> : null
}
export function ErrorNotice({ error }: { error: unknown }) { return error ? <p role="alert" className="rounded-md border border-destructive/30 bg-destructive/5 p-3 text-sm text-destructive">{catalogError(error)}</p> : null }
export function FormField({ label, children }: { label: string; children: ReactNode }) { return <label className="block space-y-2 text-sm font-medium"><span>{label}</span>{children}</label> }
export function SaveButton({ pending, disabled = false, children = '保存' }: { pending: boolean; disabled?: boolean; children?: ReactNode }) { return <Button type="submit" disabled={pending || disabled}>{pending ? '正在提交…' : children}</Button> }

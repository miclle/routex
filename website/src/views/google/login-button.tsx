import GitHubLoginButton from '@/views/github/login-button'
import type { ComponentProps } from 'react'

export default function GoogleLoginButton(
  props: Omit<ComponentProps<typeof GitHubLoginButton>, 'method'>,
) {
  return <GitHubLoginButton {...props} method="google" />
}

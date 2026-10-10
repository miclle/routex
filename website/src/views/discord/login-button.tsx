import GitHubLoginButton from '@/views/github/login-button'
import type { ComponentProps } from 'react'

export default function DiscordLoginButton(
  props: Omit<ComponentProps<typeof GitHubLoginButton>, 'method'>,
) {
  return <GitHubLoginButton {...props} method="discord" />
}

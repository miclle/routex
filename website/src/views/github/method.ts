import * as github from '@/api/github'
import * as google from '@/api/google'
import * as discord from '@/api/discord'

export type NamedIdentityMethod = 'github' | 'google' | 'discord'

// Only these reviewed fixed profiles share this view composition.
export function namedIdentityMethod(method: NamedIdentityMethod) {
  switch (method) {
    case 'github':
      return {
        getConfig: github.getGitHubConfig,
        saveConfig: github.saveGitHubConfig,
        setStatus: github.setGitHubStatus,
        beginProof: github.beginGitHubProof,
        validConfig: github.validGitHubConfig,
        validProof: github.validGitHubProof,
        getIdentity: github.getGitHubIdentity,
        unlink: github.unlinkGitHub,
        getMethod: github.getGitHubMethod,
        start: github.startGitHub,
        readSession: github.readGitHubSession,
        complete: github.completeGitHub,
        abandon: github.abandonGitHub,
        RequestError: github.GitHubRequestError,
      }
    case 'google':
      return {
        getConfig: google.getGoogleConfig,
        saveConfig: google.saveGoogleConfig,
        setStatus: google.setGoogleStatus,
        beginProof: google.beginGoogleProof,
        validConfig: google.validGoogleConfig,
        validProof: google.validGoogleProof,
        getIdentity: google.getGoogleIdentity,
        unlink: google.unlinkGoogle,
        getMethod: google.getGoogleMethod,
        start: google.startGoogle,
        readSession: google.readGoogleSession,
        complete: google.completeGoogle,
        abandon: google.abandonGoogle,
        RequestError: google.GoogleRequestError,
      }
    case 'discord':
      return {
        getConfig: discord.getDiscordConfig,
        saveConfig: discord.saveDiscordConfig,
        setStatus: discord.setDiscordStatus,
        beginProof: discord.beginDiscordProof,
        validConfig: discord.validDiscordConfig,
        validProof: discord.validDiscordProof,
        getIdentity: discord.getDiscordIdentity,
        unlink: discord.unlinkDiscord,
        getMethod: discord.getDiscordMethod,
        start: discord.startDiscord,
        readSession: discord.readDiscordSession,
        complete: discord.completeDiscord,
        abandon: discord.abandonDiscord,
        RequestError: discord.DiscordRequestError,
      }
    default:
      throw new Error('Unsupported named identity method')
  }
}

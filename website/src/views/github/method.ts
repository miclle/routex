import * as github from '@/api/github'
import * as google from '@/api/google'

export type NamedIdentityMethod = 'github' | 'google'

// Only these two reviewed profiles share this view composition.
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
  }
}

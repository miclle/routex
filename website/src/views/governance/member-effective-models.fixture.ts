import type {
  MemberEffectiveModelsAuthority,
  MemberEffectiveModelsPage,
  MemberEffectiveModelRow,
} from '@/types/member-effective-models'

export const effectiveModelAuthority = { teams: true, providers: true, prices: true }
export function effectiveModelRow(
  authority: MemberEffectiveModelsAuthority = effectiveModelAuthority,
): MemberEffectiveModelRow {
  const personal = {
    kind: 'personal' as const,
    team_id: null,
    team_name: null,
    protocols: ['openai_chat' as const],
    availability: 'ready' as const,
  }
  return {
    id: 'mdl_one',
    name: 'controlled-model',
    status: 'active',
    type: null,
    providers: authority.providers ? ['Recorded Provider'] : null,
    protocols: authority.teams
      ? ['anthropic_messages', 'gemini_generate_content', 'openai_chat', 'openai_responses']
      : ['openai_chat'],
    availability: 'ready',
    input_price: authority.prices
      ? { state: 'priced', rate: { amount: '0', unit: '1M_TOKEN', currency: 'USD' } }
      : { state: 'unauthorized', rate: null },
    output_price: authority.prices
      ? {
          state: 'priced',
          rate: { amount: '0.123456789012345678', unit: '1M_TOKEN', currency: 'EUR' },
        }
      : { state: 'unauthorized', rate: null },
    created_at: '2026-10-01T00:00:00Z',
    updated_at: null,
    sources: authority.teams
      ? [
          personal,
          {
            kind: 'team',
            team_id: 'tea_one',
            team_name: 'Recorded Team',
            protocols: ['anthropic_messages', 'gemini_generate_content', 'openai_responses'],
            availability: 'ready',
          },
        ]
      : [personal],
  }
}
export function effectiveModelsPage(
  target = 'usr_target',
  authority: MemberEffectiveModelsAuthority = effectiveModelAuthority,
): MemberEffectiveModelsPage {
  return {
    user_id: target,
    observed_at: '2026-10-05T00:00:00Z',
    subject_status: 'active',
    team_enrichment: authority.teams ? 'included' : 'not_authorized',
    union_completeness: authority.teams ? 'complete' : 'unknown',
    items: [effectiveModelRow(authority)],
  }
}

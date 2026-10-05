import { afterEach, describe, expect, it, vi } from 'vitest'
import client from './client'
import { getMemberEffectiveModels, validateMemberEffectiveModels } from './member-effective-models'
import {
  effectiveModelAuthority as flags,
  effectiveModelsPage,
} from '@/views/governance/member-effective-models.fixture'

const target = 'usr_target'
const noTeam = { ...flags, teams: false }
afterEach(() => vi.restoreAllMocks())
it('binds one safe retained target and private GET without queries or extra reads', async () => {
  const signal = new AbortController().signal
  const get = vi.spyOn(client, 'get').mockResolvedValue({
    data: effectiveModelsPage(),
    headers: { 'cache-control': 'private, no-store' },
  })
  const data = await getMemberEffectiveModels(target, flags, signal)
  expect(get).toHaveBeenCalledExactlyOnceWith('/admin/members/usr_target/effective-models', {
    signal,
  })
  expect(data.items[0].output_price.rate?.amount).toBe('0.123456789012345678')
})
it.each(['../target', 'usr_target ', '', 'a'.repeat(31), '目标'])(
  'rejects invalid target %s before transport',
  async (bad) => {
    const get = vi.spyOn(client, 'get')
    await expect(getMemberEffectiveModels(bad, flags)).rejects.toThrow()
    expect(get).not.toHaveBeenCalled()
  },
)
it('rejects cacheable responses and propagates server errors', async () => {
  const get = vi
    .spyOn(client, 'get')
    .mockResolvedValue({ data: effectiveModelsPage(), headers: { 'cache-control': 'public' } })
  await expect(getMemberEffectiveModels(target, flags)).rejects.toThrow()
  get.mockRejectedValue(new Error('controlled unavailable'))
  await expect(getMemberEffectiveModels(target, flags)).rejects.toThrow('controlled unavailable')
})
it('retains Personal-only unknown completeness with independent optional disclosures', () => {
  const authority = { teams: false, providers: false, prices: false }
  const data = validateMemberEffectiveModels(
    effectiveModelsPage(target, authority),
    target,
    authority,
  )
  expect(data.team_enrichment).toBe('not_authorized')
  expect(data.union_completeness).toBe('unknown')
  expect(data.items[0].sources).toHaveLength(1)
  expect(data.items[0].providers).toBeNull()
  expect(data.items[0].input_price).toEqual({ state: 'unauthorized', rate: null })
})
it('preserves same-named Teams by stable ID and all native protocol values', () => {
  const p = effectiveModelsPage()
  p.items[0].sources.push({ ...p.items[0].sources[1], team_id: 'tea_two' })
  expect(validateMemberEffectiveModels(p, target, flags).items[0].sources).toHaveLength(3)
  expect(p.items[0].protocols).toHaveLength(4)
})
describe('fail-closed DTO boundary', () => {
  it.each([
    [
      'target',
      (p: ReturnType<typeof effectiveModelsPage>) => {
        p.user_id = 'usr_peer'
      },
    ],
    [
      'future-created',
      (p: ReturnType<typeof effectiveModelsPage>) => {
        p.items[0].created_at = '2027-01-01T00:00:00Z'
      },
    ],
    [
      'type fiction',
      (p: ReturnType<typeof effectiveModelsPage>) => {
        Object.assign(p.items[0], { type: 'Chat' })
      },
    ],
    [
      'updated fiction',
      (p: ReturnType<typeof effectiveModelsPage>) => {
        Object.assign(p.items[0], { updated_at: p.observed_at })
      },
    ],
    [
      'extra receipt',
      (p: ReturnType<typeof effectiveModelsPage>) => {
        Object.assign(p, { receipt: 'historical' })
      },
    ],
    [
      'selectable',
      (p: ReturnType<typeof effectiveModelsPage>) => {
        Object.assign(p.items[0], { selectable: true })
      },
    ],
    [
      'duplicate model',
      (p: ReturnType<typeof effectiveModelsPage>) => {
        p.items.push(p.items[0])
      },
    ],
    [
      'duplicate Personal',
      (p: ReturnType<typeof effectiveModelsPage>) => {
        p.items[0].sources.unshift(p.items[0].sources[0])
      },
    ],
    [
      'duplicate Team',
      (p: ReturnType<typeof effectiveModelsPage>) => {
        p.items[0].sources.push(p.items[0].sources[1])
      },
    ],
    [
      'missing source',
      (p: ReturnType<typeof effectiveModelsPage>) => {
        p.items[0].sources = []
      },
    ],
    [
      'Personal Team ID',
      (p: ReturnType<typeof effectiveModelsPage>) => {
        p.items[0].sources[0].team_id = 'tea_hidden'
      },
    ],
    [
      'source order',
      (p: ReturnType<typeof effectiveModelsPage>) => {
        p.items[0].sources.reverse()
      },
    ],
    [
      'unknown protocol',
      (p: ReturnType<typeof effectiveModelsPage>) => {
        Object.assign(p.items[0].sources[0], { protocols: ['unknown'] })
      },
    ],
    [
      'protocol union',
      (p: ReturnType<typeof effectiveModelsPage>) => {
        p.items[0].protocols = ['openai_chat']
      },
    ],
    [
      'unready protocols',
      (p: ReturnType<typeof effectiveModelsPage>) => {
        p.items[0].sources[1].availability = 'unknown'
      },
    ],
    [
      'wrong union availability',
      (p: ReturnType<typeof effectiveModelsPage>) => {
        p.items[0].availability = 'unknown'
      },
    ],
    [
      'inactive readiness',
      (p: ReturnType<typeof effectiveModelsPage>) => {
        p.subject_status = 'disabled'
      },
    ],
    [
      'disabled readiness',
      (p: ReturnType<typeof effectiveModelsPage>) => {
        p.items[0].status = 'disabled'
      },
    ],
    [
      'unpriced rate',
      (p: ReturnType<typeof effectiveModelsPage>) => {
        Object.assign(p.items[0].input_price, { state: 'missing' })
      },
    ],
    [
      'numeric amount',
      (p: ReturnType<typeof effectiveModelsPage>) => {
        Object.assign(p.items[0].input_price.rate!, { amount: 0.1 })
      },
    ],
    [
      'unsupported unit',
      (p: ReturnType<typeof effectiveModelsPage>) => {
        Object.assign(p.items[0].input_price.rate!, { unit: '1_TOKEN' })
      },
    ],
  ] as const)('rejects %s', (_, change) => {
    const p = effectiveModelsPage()
    change(p)
    expect(() => validateMemberEffectiveModels(p, target, flags)).toThrow()
  })
})
it.each(['Team sources', 'complete union', 'Team-only model', 'Team protocol overlap'])(
  'rejects unauthorized %s before cache',
  (scenario) => {
    const p = effectiveModelsPage(target, noTeam)
    if (scenario === 'Team sources')
      p.items[0].sources.push(effectiveModelsPage().items[0].sources[1])
    if (scenario === 'complete union') p.union_completeness = 'complete'
    if (scenario === 'Team-only model')
      p.items[0].sources = [effectiveModelsPage().items[0].sources[1]]
    if (scenario === 'Team protocol overlap') p.items[0].protocols.push('openai_responses')
    expect(() => validateMemberEffectiveModels(p, target, noTeam)).toThrow()
  },
)
it.each(['providers', 'prices'] as const)(
  'rejects independent unauthorized %s metadata',
  (kind) => {
    const authority = { ...flags, [kind]: false }
    expect(() => validateMemberEffectiveModels(effectiveModelsPage(), target, authority)).toThrow()
  },
)
it('preserves unknown and inactive states without fabricated route/price readiness', () => {
  for (const subjectStatus of ['active', 'disabled', 'offboarded'] as const) {
    const p = effectiveModelsPage()
    p.subject_status = subjectStatus
    const state = subjectStatus === 'active' ? 'unknown' : 'unavailable'
    p.items[0].availability = state
    p.items[0].protocols = []
    p.items[0].input_price = p.items[0].output_price = { state: 'unavailable', rate: null }
    p.items[0].sources.forEach((s) => {
      s.availability = state
      s.protocols = []
    })
    expect(validateMemberEffectiveModels(p, target, flags).subject_status).toBe(subjectStatus)
  }
})
it('accepts the complete supported 1000 Personal +5000 Team source result', () => {
  const p = effectiveModelsPage()
  const template = p.items[0]
  p.items = Array.from({ length: 1000 }, (_, i) => ({
    ...template,
    id: 'mdl_' + String(i).padStart(4, '0'),
    sources: [
      template.sources[0],
      ...Array.from({ length: 5 }, (_, j) => ({ ...template.sources[1], team_id: 'tea_' + j })),
    ],
  }))
  expect(validateMemberEffectiveModels(p, target, flags).items).toHaveLength(1000)
  p.items[0].sources.push({ ...template.sources[1], team_id: 'tea_5' })
  expect(() => validateMemberEffectiveModels(p, target, flags)).toThrow()
  p.items[0].sources.pop()
  p.items.push({ ...template, id: 'mdl_z' })
  expect(() => validateMemberEffectiveModels(p, target, flags)).toThrow()
})

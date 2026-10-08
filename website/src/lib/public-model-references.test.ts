import { describe, expect, it } from 'vitest'
import data from '../../../internal/routex/modelreferences/public-model-references.v1.json'
import {
  publicModelReferences,
  readPublicModelReferences,
  searchPublicModelNames,
} from './public-model-references'

describe('versioned advisory public Model identities', () => {
  it('contains only exact reviewed identities and factual source metadata', () => {
    expect(publicModelReferences.map((row) => row.name)).toEqual([
      'gpt-5.2',
      'gpt-5.2-2025-12-11',
      'claude-sonnet-4-6',
      'gemini-2.5-flash',
    ])
    expect(publicModelReferences.every((row) => row.reviewed_at === '2026-10-04')).toBe(true)
    expect(Object.isFrozen(publicModelReferences)).toBe(true)
    expect(Object.isFrozen(publicModelReferences[0])).toBe(true)
  })
  it.each([
    null,
    { ...data, version: 2 },
    { ...data, capabilities: ['image'] },
    { ...data, entries: Array(101).fill(data.entries[0]) },
    { ...data, entries: [data.entries[0], data.entries[0]] },
    { ...data, entries: [{ ...data.entries[0], price: '0' }] },
    { ...data, entries: [{ ...data.entries[0], name: ' leading-space' }] },
    { ...data, entries: [{ ...data.entries[0], name: 'x'.repeat(129) }] },
    { ...data, entries: [{ ...data.entries[0], reviewed_at: '2026-02-30' }] },
    { ...data, entries: [{ ...data.entries[0], source_url: 'https://unknown.invalid/model' }] },
    {
      ...data,
      entries: [{ ...data.entries[0], source_url: 'http://developers.openai.com/model' }],
    },
    {
      ...data,
      entries: [{ ...data.entries[0], source_url: 'https://secret@developers.openai.com/model' }],
    },
    {
      ...data,
      entries: [
        { ...data.entries[0], source_url: 'https://developers.openai.com/model?key=private' },
      ],
    },
    { ...data, entries: [{ ...data.entries[0], source_url: data.entries[0].source_url + ' ' }] },
  ])('malformed or unknown metadata disables suggestions only (%#)', (file) => {
    const rows = readPublicModelReferences(file)
    expect(rows).toEqual([])
    expect(searchPublicModelNames('custom/Exact', [], rows)).toEqual([])
  })
  it('searches literals without rewriting exact identities and bounds the list', () => {
    expect(searchPublicModelNames('GPT-5.2')).toEqual(['gpt-5.2', 'gpt-5.2-2025-12-11'])
    expect(searchPublicModelNames('gpt', ['gpt-5.2'])).toEqual(['gpt-5.2-2025-12-11'])
    expect(searchPublicModelNames('gpt', ['GPT-5.2'])).toHaveLength(2)
    expect(searchPublicModelNames('.*')).toEqual([])
    expect(searchPublicModelNames('x'.repeat(129))).toEqual([])
    const many = readPublicModelReferences({
      ...data,
      entries: Array.from({ length: 100 }, (_, i) => ({ ...data.entries[0], name: `model-${i}` })),
    })
    expect(searchPublicModelNames('', [], many)).toHaveLength(8)
  })
})

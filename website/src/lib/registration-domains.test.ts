import { expect, it } from 'vitest'
import {
  addRegistrationDomain,
  canonicalRegistrationDomain,
  isCanonicalRegistrationDomains,
} from './registration-domains'

it.each([
  ' Example.INVALID ',
  'xn--bcher-kva.example',
  'corp.123',
  '1.corp',
  'sub.example.invalid',
])('canonicalizes only explicit ASCII domain %s', (value) => {
  expect(canonicalRegistrationDomain(value)).toBe(value.replace(/^ +| +$/g, '').toLowerCase())
})
it.each([
  'example',
  '',
  'a..example',
  '.example.invalid',
  'example.invalid.',
  '@example.invalid',
  'person@example.invalid',
  'https://example.invalid',
  'example.invalid:443',
  '*.example.invalid',
  'a_b.example',
  '-a.example',
  'a-.example',
  'a b.example',
  '\texample.invalid',
  'example.invalid\n',
  'bücher.example',
  'K.example',
  '127.0.0.1',
  '127.0.0.01',
  '127.1',
  '999.999.999.999',
  '[::1]',
  'a'.repeat(64) + '.example',
  ['a'.repeat(63), 'b'.repeat(63), 'c'.repeat(63), 'd'.repeat(62)].join('.'),
])('rejects unsafe domain %j without lookup or normalization', (value) => {
  expect(canonicalRegistrationDomain(value)).toBeNull()
})
it('keeps exact subdomain entries separate, sorts additions, rejects normalized duplicates and preserves input arrays', () => {
  const original = ['example.invalid']
  expect(addRegistrationDomain(original, ' EXAMPLE.INVALID ')).toEqual({ kind: 'duplicate' })
  expect(addRegistrationDomain(original, ' sub.example.invalid ')).toEqual({
    kind: 'added',
    domains: ['example.invalid', 'sub.example.invalid'],
  })
  expect(original).toEqual(['example.invalid'])
  expect(isCanonicalRegistrationDomains(['sub.example.invalid', 'example.invalid'])).toBe(false)
  expect(isCanonicalRegistrationDomains(['example.invalid', 'example.invalid'])).toBe(false)
  expect(isCanonicalRegistrationDomains(['Example.invalid'])).toBe(false)
  expect(isCanonicalRegistrationDomains([])).toBe(true)
})
it('enforces independent full array count and ASCII JSON byte bounds', () => {
  const count = Array.from(
    { length: 32 },
    (_, index) => `d${String(index).padStart(2, '0')}.example`,
  )
  expect(isCanonicalRegistrationDomains(count)).toBe(true)
  expect(addRegistrationDomain(count, 'new.example')).toEqual({ kind: 'limit' })
  const bytes = Array.from(
    { length: 32 },
    (_, index) => `${'a'.repeat(60)}${String(index).padStart(2, '0')}.example`,
  )
  expect(bytes.every((domain) => canonicalRegistrationDomain(domain) === domain)).toBe(true)
  expect(JSON.stringify(bytes).length).toBeGreaterThan(2048)
  expect(isCanonicalRegistrationDomains(bytes)).toBe(false)
  expect(
    canonicalRegistrationDomain(
      ['a'.repeat(63), 'b'.repeat(63), 'c'.repeat(63), 'd'.repeat(61)].join('.'),
    ),
  ).not.toBeNull()
})

import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, expect, it } from 'vitest'
import i18n from '@/i18n'
import ProviderQualityResult from './provider-quality'
import { qualityFixture } from './provider-quality-fixture'
import type { ProviderQualityReport } from '@/types/insights'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
let host: HTMLDivElement, root: Root
beforeEach(async () => {
  await i18n.changeLanguage('en')
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
})
afterEach(async () => {
  await act(async () => root.unmount())
  host.remove()
  await i18n.changeLanguage('en')
})
it('retains three summary cards and displays mean separately from P95 with known and unknown samples', async () => {
  const report = qualityFixture()
  report.summary.average_duration_ms = report.providers[0].average_duration_ms = 150.125
  await act(async () => root.render(<ProviderQualityResult report={report} />))
  const cards = host.querySelector('.grid')!
  expect(cards.children).toHaveLength(3)
  expect(cards.textContent).toContain(i18n.t('insights:qualityMean'))
  expect(cards.textContent).toContain('150.125 ms')
  expect(cards.textContent).toContain('1 known / 1 unknown')
  expect(host.textContent).toContain(i18n.t('insights:qualityMeanDefinition'))
  const headers = [...host.querySelectorAll('th')].map((node) => node.textContent)
  expect(headers).toContain(i18n.t('insights:qualityMean'))
  expect(headers).toContain(i18n.t('insights:qualityP95'))
  const cells = [...host.querySelectorAll('tbody td')]
  expect(cells[8].textContent).toBe('150.125 ms')
  expect(cells[9].textContent).toBe('150 ms')
  await act(async () => i18n.changeLanguage('zh'))
  expect(cards.textContent).toContain(i18n.t('insights:qualityMean'))
  expect(host.textContent).toContain(i18n.t('insights:qualityMeanDefinition'))
  expect(host.textContent).not.toContain('Mean full-attempt duration')
})
it('keeps recorded zero distinct from unavailable mean and never replaces unknown samples with zero', async () => {
  const report = qualityFixture()
  report.summary.average_duration_ms = report.providers[0].average_duration_ms = 0
  await act(async () => root.render(<ProviderQualityResult report={report} />))
  expect(host.querySelector('.grid')!.textContent).toContain('0 ms')
  for (const counts of [report.summary, ...report.providers]) {
    counts.average_duration_ms = counts.p95_duration_ms = null
    counts.known_duration = 0
    counts.unknown_duration = counts.eligible
  }
  await act(async () => root.render(<ProviderQualityResult report={report} />))
  const cards = host.querySelector('.grid')!
  expect(cards.textContent).toContain(i18n.t('insights:qualityUnknown'))
  expect(cards.textContent).toContain('0 known / 2 unknown')
  expect(cards.textContent).not.toContain('0 ms')
})

function observations() {
  return host.querySelector<HTMLElement>(
    `section[aria-label="${i18n.t('insights:qualityObservationsTitle')}"]`,
  )!
}
function emptyQuality(): ProviderQualityReport {
  const report = qualityFixture()
  report.total_attempts = report.unknown_attribution = 0
  report.latest_completed_at = null
  report.providers = []
  report.summary = {
    attempts: 0,
    eligible: 0,
    excluded: 0,
    credential_rejected: 0,
    successes: 0,
    success_rate_bps: null,
    rate_limited: 0,
    server_errors: 0,
    known_duration: 0,
    unknown_duration: 0,
    average_duration_ms: null,
    p95_duration_ms: null,
  }
  return report
}

it('places captured read-only observations after the Provider table and keeps overlapping attempt categories separate', async () => {
  const report = emptyQuality()
  report.total_attempts = 1
  Object.assign(report.summary, {
    attempts: 1,
    eligible: 1,
    success_rate_bps: 0,
    rate_limited: 1,
    server_errors: 1,
    known_duration: 1,
    average_duration_ms: 0,
    p95_duration_ms: 0,
  })
  report.providers = [{ ...report.summary, provider_id: 'prv_recorded', name: 'Recorded Provider' }]
  await act(async () => root.render(<ProviderQualityResult report={report} />))
  const section = observations()
  expect(section.previousElementSibling?.querySelector('table')).not.toBeNull()
  expect([...section.querySelectorAll('li')].map((item) => item.textContent)).toEqual([
    i18n.t('insights:qualityObservationRateLimited', { value: '1' }),
    i18n.t('insights:qualityObservationServerErrors', { value: '1' }),
  ])
  expect(section.textContent).toContain(i18n.t('insights:qualityObservationsDefinition'))
  expect(section.querySelector('button, a, input, form')).toBeNull()
  expect(host.querySelector('.grid')!.children).toHaveLength(3)
})

it('shows unknown attribution separately from eligible duration gaps and updates observations on language changes', async () => {
  const report = qualityFixture()
  report.summary.rate_limited = report.providers[0].rate_limited = 0
  await act(async () => root.render(<ProviderQualityResult report={report} />))
  expect([...observations().querySelectorAll('li')].map((item) => item.textContent)).toEqual([
    i18n.t('insights:qualityObservationAttribution', { value: '1' }),
    i18n.t('insights:qualityObservationDuration', { value: '1' }),
  ])
  await act(async () => i18n.changeLanguage('zh'))
  expect(observations().textContent).toContain(
    i18n.t('insights:qualityObservationAttribution', { value: '1' }),
  )
  expect(observations().textContent).toContain(
    i18n.t('insights:qualityObservationDuration', { value: '1' }),
  )
  expect(observations().textContent).not.toContain('Recorded rate-limited attempts')
  expect(observations().textContent).not.toContain('Eligible attributed attempts')
})

it('distinguishes an empty capture from zero recorded signals and removes observations from an earlier capture', async () => {
  await act(async () => root.render(<ProviderQualityResult report={qualityFixture()} />))
  expect(observations().querySelectorAll('li')).toHaveLength(3)
  const report = emptyQuality()
  await act(async () => root.render(<ProviderQualityResult report={report} />))
  expect(observations().textContent).toContain(i18n.t('insights:qualityObservationsEmpty'))
  expect(observations().textContent).not.toContain(i18n.t('insights:qualityObservationsNone'))
  expect(observations().querySelectorAll('li')).toHaveLength(0)

  report.total_attempts = 1
  report.summary.attempts = report.summary.excluded = 1
  report.providers = [{ ...report.summary, provider_id: 'prv_recorded', name: '' }]
  await act(async () => root.render(<ProviderQualityResult report={report} />))
  expect(observations().textContent).toContain(i18n.t('insights:qualityObservationsNone'))
  expect(observations().textContent).not.toContain(i18n.t('insights:qualityObservationsEmpty'))
  expect(observations().querySelectorAll('li')).toHaveLength(0)
})

it('does not treat an unattributed-only capture as empty or synthesize missing eligible duration samples', async () => {
  const report = emptyQuality()
  report.total_attempts = report.unknown_attribution = 2
  await act(async () => root.render(<ProviderQualityResult report={report} />))
  expect([...observations().querySelectorAll('li')].map((item) => item.textContent)).toEqual([
    i18n.t('insights:qualityObservationAttribution', { value: '2' }),
  ])
  expect(observations().textContent).not.toContain(i18n.t('insights:qualityObservationsEmpty'))
  expect(observations().textContent).not.toContain(i18n.t('insights:qualityObservationsNone'))
})

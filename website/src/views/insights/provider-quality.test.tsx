import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, expect, it } from 'vitest'
import i18n from '@/i18n'
import ProviderQualityResult from './provider-quality'
import { qualityFixture } from './provider-quality-fixture'

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

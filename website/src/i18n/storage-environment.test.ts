import { afterEach, describe, expect, it } from 'vitest'

afterEach(() => {
  localStorage.clear()
  sessionStorage.clear()
})

describe('jsdom browser storage environment', () => {
  it('uses the DOM Storage implementation through both Window and global references', () => {
    expect(localStorage).toBe(window.localStorage)
    expect(sessionStorage).toBe(window.sessionStorage)
    expect(localStorage).toBeInstanceOf(Storage)
    expect(sessionStorage).toBeInstanceOf(Storage)
    expect(localStorage).not.toBe(sessionStorage)
  })

  it('preserves literal browser values, enumeration and removal without a storage stub', () => {
    localStorage.setItem('routex.test', '0.000000000000000001')
    sessionStorage.setItem('routex.test', 'transient')
    expect(window.localStorage.getItem('routex.test')).toBe('0.000000000000000001')
    expect(window.sessionStorage.getItem('routex.test')).toBe('transient')
    expect(Object.keys(localStorage)).toEqual(['routex.test'])
    expect(localStorage.length).toBe(1)
    localStorage.removeItem('routex.test')
    expect(localStorage.getItem('routex.test')).toBeNull()
    expect(localStorage.length).toBe(0)
    expect(sessionStorage.getItem('routex.test')).toBe('transient')
  })
})

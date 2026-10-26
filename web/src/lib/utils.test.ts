import { describe, expect, it } from 'vitest'
import { date } from './utils'
describe('honest publication date precision', () => {
  it('retains year-only evidence without inventing January 1', () =>
    expect(date('2025')).toBe('2025'))
  it('retains month-only evidence without inventing a day', () =>
    expect(date('2025-03')).toBe('2025-03'))
  it('preserves ambiguous or unparseable original evidence without throwing', () => {
    expect(date('Spring 2025')).toBe('Spring 2025')
    expect(date('日期待复核')).toBe('日期待复核')
  })
  it('does not normalize impossible calendar days into another month', () => {
    expect(date('2025-02-29')).toBe('2025-02-29')
    expect(date('2025-02-30T00:00:00Z')).toBe('2025-02-30T00:00:00Z')
    expect(date('2025-13-01')).toBe('2025-13-01')
  })
  it('formats a valid full UTC day', () => {
    expect(date('2026-10-26T00:00:00Z')).toBe('2026/10/26')
    expect(date('2024-02-29')).toBe('2024/02/29')
  })
  it('uses UTC consistently for timezone-qualified timestamps', () =>
    expect(date('2026-10-26T02:00:00+08:00')).toBe('2026/10/25'))
  it('keeps missing and Go zero dates pending', () => {
    expect(date(null)).toBe('待核验')
    expect(date('')).toBe('待核验')
    expect(date('0001-01-01T00:00:00Z')).toBe('待核验')
  })
})

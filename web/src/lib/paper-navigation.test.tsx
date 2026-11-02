import { act, renderHook, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { paperLink, usePaperNavigation, validPaperId } from './paper-navigation'

beforeEach(() => window.history.replaceState(null, '', '/'))
describe('paper navigation', () => {
  it('keeps large IDs exact and shares only the paper parameter', () => {
    window.history.replaceState(null, '', '/?q=private&token=secret#notes')
    expect(paperLink('9223372036854775807')).toBe(`${location.origin}/?paper=9223372036854775807`)
    expect(validPaperId('9223372036854775807')).toBe(true)
    for (const id of ['', '0', '-1', '1/2', '1e5', '9223372036854775808'])
      expect(validPaperId(id)).toBe(false)
  })
  it.each([
    '/workspace/glean/',
    '//other.example/path',
    '//user:pass@other.example/path',
    '/workspace/%E8%AE%BA%E6%96%87%20notes/%2F%3F%23%25',
    '/%2F%2Fother.example/path',
  ])('keeps canonical links on the current origin at %s', (pathname) => {
    // Use an absolute same-origin URL so // remains a pathname, not an authority.
    history.replaceState(null, '', `${location.origin}${pathname}?q=private&token=secret#notes`)
    const link = new URL(paperLink('9223372036854775807'))
    expect(link.origin).toBe(location.origin)
    expect(link.pathname).toBe(pathname)
    expect(link.search).toBe('?paper=9223372036854775807')
    expect(link.hash).toBe('')
    expect(link.username).toBe('')
    expect(link.password).toBe('')
  })
  it('does not retain credentials from the current URL', () => {
    const origin = location.origin
    vi.stubGlobal('window', {
      location: new URL(
        `${location.protocol}//user:pass@${location.host}/workspace/?token=secret#notes`,
      ),
    })
    expect(paperLink('123')).toBe(`${origin}/workspace/?paper=123`)
  })
  it('opens details on a double-slash path without crossing origins', () => {
    history.replaceState(null, '', `${location.origin}//other.example/path?token=secret#notes`)
    const { result } = renderHook(usePaperNavigation)
    act(() => result.current.open('9223372036854775807'))
    expect(result.current.id).toBe('9223372036854775807')
    expect(location.href).toBe(`${location.origin}//other.example/path?paper=9223372036854775807`)
  })
  it('pushes once, traverses Back/Forward, and closes owned list entries with Back', async () => {
    const { result } = renderHook(usePaperNavigation)
    const length = history.length
    act(() => {
      result.current.open('123')
      result.current.open('123')
    })
    expect(history.length).toBe(length + 1)
    expect(result.current.id).toBe('123')
    act(() => history.back())
    await waitFor(() => expect(result.current.id).toBeNull())
    act(() => history.forward())
    await waitFor(() => expect(result.current.id).toBe('123'))
    const back = vi.spyOn(history, 'back')
    act(() => {
      result.current.close()
      result.current.close()
    })
    expect(back).toHaveBeenCalledTimes(1)
    await waitFor(() => expect(location.search).toBe(''))
    back.mockRestore()
  })
  it('replaces a direct landing rather than leaving the app', () => {
    history.replaceState(null, '', '/?paper=123')
    const { result } = renderHook(usePaperNavigation)
    const back = vi.spyOn(history, 'back')
    act(() => result.current.close())
    expect(back).not.toHaveBeenCalled()
    expect(location.search).toBe('')
    expect(result.current.id).toBeNull()
    back.mockRestore()
  })
  it('does not trust history markers after workspace/account remount', () => {
    const first = renderHook(usePaperNavigation)
    act(() => first.result.current.open('123'))
    first.unmount()
    const next = renderHook(usePaperNavigation)
    const back = vi.spyOn(history, 'back')
    act(() => next.result.current.close())
    expect(back).not.toHaveBeenCalled()
    expect(location.search).toBe('')
    back.mockRestore()
  })
  it('clears invalid and empty targets without pushing history', () => {
    history.replaceState({ unrelated: 'keep' }, '', '/?paper=')
    const { result } = renderHook(usePaperNavigation)
    expect(result.current.id).toBe('')
    const length = history.length
    act(() => result.current.clear())
    expect(history.length).toBe(length)
    expect(history.state).toEqual({ unrelated: 'keep' })
    expect(location.search).toBe('')
  })
})

it('works without secure-context randomUUID and initializes ownership only once per mount', async () => {
  const getRandomValues = vi.fn(crypto.getRandomValues.bind(crypto))
  vi.stubGlobal('crypto', { getRandomValues })
  expect(crypto.randomUUID).toBeUndefined()
  const first = renderHook(usePaperNavigation)
  const back = vi.spyOn(history, 'back')
  act(() => first.result.current.open('123'))
  first.rerender()
  expect(getRandomValues).toHaveBeenCalledTimes(1)
  act(() => first.result.current.close())
  expect(back).toHaveBeenCalledTimes(1)
  await waitFor(() => expect(location.search).toBe(''))
  act(() => first.result.current.open('123'))
  const savedOwner = history.state.gleanPaperNavigation.owner
  first.unmount()
  back.mockClear()
  const next = renderHook(usePaperNavigation)
  expect(getRandomValues).toHaveBeenCalledTimes(2)
  act(() => next.result.current.close())
  expect(back).not.toHaveBeenCalled()
  act(() => next.result.current.open('123'))
  expect(history.state.gleanPaperNavigation.owner).not.toBe(savedOwner)
  expect(getRandomValues).toHaveBeenCalledTimes(2)
  back.mockRestore()
})

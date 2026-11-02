import { useEffect, useRef, useState } from 'react'

const marker = 'gleanPaperNavigation'
// paperLink 仅分享论文 ID，不带搜索、个人信息或会话参数。
export function paperLink(id: string): string {
  const url = new URL(window.location.pathname, window.location.origin)
  url.searchParams.set('paper', id)
  return url.href
}
// validPaperId 与后端正整数 int64 ID 契约一致，全程避免 Number 精度转换。
export function validPaperId(id: string): boolean {
  // IDs remain strings, including Snowflake values beyond Number.MAX_SAFE_INTEGER.
  return /^[1-9]\d{0,18}$/.test(id) && (id.length < 19 || id <= '9223372036854775807')
}
const readPaper = () => new URL(window.location.href).searchParams.get('paper')

// usePaperNavigation 在当前工作台内管理详情历史，不持久化个人筛选。
export function usePaperNavigation() {
  const [id, setId] = useState<string | null>(readPaper)
  // Deliberately memory-only: after reload/login a saved history marker is not
  // proof that this workspace owns the previous entry (which may be external).
  // getRandomValues 也支持局域网 HTTP；randomUUID 仅限安全上下文。
  const [owner] = useState(() =>
    Array.from(crypto.getRandomValues(new Uint32Array(4)), (value) =>
      value.toString(16).padStart(8, '0'),
    ).join(''),
  )
  const closing = useRef(false)
  useEffect(() => {
    const sync = () => {
      closing.current = false
      setId(readPaper())
    }
    window.addEventListener('popstate', sync)
    sync()
    return () => window.removeEventListener('popstate', sync)
  }, [])
  const clear = () => {
    const url = new URL(window.location.href)
    url.searchParams.delete('paper')
    const state = { ...window.history.state }
    delete state[marker]
    window.history.replaceState(state, '', url)
    closing.current = false
    setId(null)
  }
  return {
    id,
    open: (next: string) => {
      if (closing.current || readPaper() === next) return
      closing.current = false
      const fromList = readPaper() === null
      window.history.pushState(
        { [marker]: fromList ? { owner, id: next } : null },
        '',
        paperLink(next),
      )
      setId(next)
    },
    clear,
    close: () => {
      if (closing.current) return
      const entry = window.history.state?.[marker]
      if (entry?.owner === owner && entry.id === readPaper()) {
        closing.current = true
        setId(null)
        window.history.back()
      } else clear()
    },
  }
}

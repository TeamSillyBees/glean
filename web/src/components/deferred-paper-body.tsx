import { Component, lazy, Suspense, useState, type ComponentType, type ReactNode } from 'react'
import type { Topic } from '../lib/types'
import { Loading } from './common'
import { Button } from './ui/button'

export type PaperBodyProps = {
  id: string
  notify: (message: string) => void
  canManage?: boolean
  topics?: Topic[]
  activeTopic?: string
}

class PaperBodyBoundary extends Component<
  { children: ReactNode; retry: () => void },
  { failed: boolean }
> {
  state = { failed: false }
  static getDerivedStateFromError() {
    return { failed: true }
  }
  render() {
    if (!this.state.failed) return this.props.children
    return (
      <section role="alert" className="space-y-3 px-6 py-10 text-center">
        <h2 className="font-semibold">论文详情暂时无法打开</h2>
        <p className="text-sm text-muted-foreground">
          请检查网络后重试，也可关闭详情继续浏览。若仍无法打开，可手动刷新页面；刷新后需要重新登录。
        </p>
        <Button variant="outline" onClick={this.props.retry}>
          重试打开详情
        </Button>
      </section>
    )
  }
}

// Only the body suspends: the eager dialog retains its title, close control and
// focus lifecycle. An import never sets selection or carries account data.
export function deferPaperBody(load: () => Promise<{ default: ComponentType<PaperBodyProps> }>) {
  let CachedBody = lazy(load)
  return function DeferredPaperBody(props: PaperBodyProps) {
    const [attempt, setAttempt] = useState(() => ({ Body: CachedBody, key: 0 }))
    const Body = attempt.Body
    return (
      <PaperBodyBoundary
        key={attempt.key}
        retry={() => {
          CachedBody = lazy(load)
          setAttempt((old) => ({ Body: CachedBody, key: old.key + 1 }))
        }}
      >
        <Suspense fallback={<Loading label="正在打开论文详情…" />}>
          <Body {...props} />
        </Suspense>
      </PaperBodyBoundary>
    )
  }
}

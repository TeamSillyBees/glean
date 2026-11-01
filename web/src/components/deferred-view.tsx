import { Component, lazy, Suspense, useState, type ComponentType, type ReactNode } from 'react'
import { Loading } from './common'
import { Button } from './ui/button'

class ViewBoundary extends Component<
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
      <section role="alert" className="space-y-3 border-y bg-card px-6 py-10 text-center">
        <h2 className="font-semibold">此页面暂时无法打开</h2>
        <p className="text-sm text-muted-foreground">
          请检查网络后重试，或从导航返回论文工作台。重试不会退出登录。
          若仍无法打开，可手动刷新页面；刷新后需要重新登录。
        </p>
        <Button variant="outline" onClick={this.props.retry}>
          重试打开页面
        </Button>
      </section>
    )
  }
}

// Each mounted view owns its retry generation. A late import cannot navigate,
// resurrect an unmounted view, or reset the authenticated workspace.
export function deferredView<P extends object>(load: () => Promise<{ default: ComponentType<P> }>) {
  let CachedView = lazy(load)
  return function DeferredView(props: P) {
    const [attempt, setAttempt] = useState(() => ({ View: CachedView, key: 0 }))
    const View = attempt.View
    return (
      <ViewBoundary
        key={attempt.key}
        retry={() => {
          CachedView = lazy(load)
          setAttempt((old) => ({ View: CachedView, key: old.key + 1 }))
        }}
      >
        <Suspense fallback={<Loading label="正在打开页面…" />}>
          <View {...props} />
        </Suspense>
      </ViewBoundary>
    )
  }
}

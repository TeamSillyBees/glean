import { useState, type ComponentType } from 'react'
import { BookOpen, Link, X } from 'lucide-react'
import { Button } from './ui/button'
import { Dialog, DialogClose, DialogContent, DialogDescription, DialogTitle } from './ui/dialog'
import { deferPaperBody, type PaperBodyProps } from './deferred-paper-body'
import type { Topic } from '../lib/types'
import { paperLink, validPaperId } from '../lib/paper-navigation'

export function createPaperDetail(load: () => Promise<{ default: ComponentType<PaperBodyProps> }>) {
  const DeferredPaperBody = deferPaperBody(load)
  return function PaperDetail({
    id,
    onClose,
    notify,
    restoreFocus,
    canManage = false,
    topics = [],
    activeTopic,
  }: {
    canManage?: boolean
    topics?: Topic[]
    activeTopic?: string
    id: string | null
    onClose: () => void
    notify: (message: string) => void
    restoreFocus?: () => void
  }) {
    const [copyFallback, setCopyFallback] = useState<string | null>(null)
    return (
      <Dialog
        open={id !== null}
        onOpenChange={(open) => {
          if (!open) onClose()
        }}
      >
        <DialogContent
          drawer
          showCloseButton={false}
          onCloseAutoFocus={(event) => {
            event.preventDefault()
            restoreFocus?.()
          }}
        >
          <div
            data-testid="paper-detail-header"
            className="sticky top-0 z-10 flex items-start gap-3 border-b bg-card px-5 py-4 sm:px-8"
          >
            <div className="min-w-0 flex-1">
              <DialogTitle className="flex items-center gap-2 text-lg font-semibold">
                <BookOpen className="size-4 text-primary" aria-hidden="true" />
                论文详情
              </DialogTitle>
              <DialogDescription className="mt-1 text-xs text-muted-foreground">
                原始证据与个人阅读状态分开保存。
              </DialogDescription>
            </div>
            {id !== null && validPaperId(id) && (
              <Button
                variant="ghost"
                size="icon"
                aria-label="复制论文链接"
                onClick={async () => {
                  try {
                    await navigator.clipboard.writeText(paperLink(id))
                    setCopyFallback(null)
                    notify('论文链接已复制；打开链接后需要登录。')
                  } catch {
                    setCopyFallback(id)
                    notify('复制失败，请使用详情中的手动复制链接。')
                  }
                }}
              >
                <Link />
              </Button>
            )}
            {/* 关闭入口与标题共用 sticky 层，滚动后仍可点击。 */}
            <DialogClose asChild>
              <Button
                variant="ghost"
                size="icon"
                className="size-11 rounded-sm text-muted-foreground"
                aria-label="关闭"
              >
                <X />
              </Button>
            </DialogClose>
          </div>
          {id !== null && copyFallback === id && (
            <label className="block border-b px-5 py-3 text-sm sm:px-8">
              手动复制论文链接
              <input
                className="form-input mt-2"
                readOnly
                value={paperLink(id)}
                onFocus={(event) => event.currentTarget.select()}
              />
            </label>
          )}
          {id !== null && !validPaperId(id) ? (
            <section role="alert" className="space-y-3 px-6 py-10 text-center">
              <h2 className="font-semibold">论文链接无效</h2>
              <p className="text-sm text-muted-foreground">请检查论文 ID，或返回列表选择论文。</p>
              <Button variant="outline" onClick={onClose}>
                返回列表
              </Button>
            </section>
          ) : (
            id !== null && (
              <DeferredPaperBody
                key={id}
                id={id}
                notify={notify}
                canManage={canManage}
                topics={topics}
                activeTopic={activeTopic}
              />
            )
          )}
        </DialogContent>
      </Dialog>
    )
  }
}

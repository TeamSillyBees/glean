import type { ComponentType } from 'react'
import { BookOpen, X } from 'lucide-react'
import { Button } from './ui/button'
import { Dialog, DialogClose, DialogContent, DialogDescription, DialogTitle } from './ui/dialog'
import { deferPaperBody, type PaperBodyProps } from './deferred-paper-body'
import type { Topic } from '../lib/types'

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
    return (
      <Dialog
        open={!!id}
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
          {id && (
            <DeferredPaperBody
              id={id}
              notify={notify}
              canManage={canManage}
              topics={topics}
              activeTopic={activeTopic}
            />
          )}
        </DialogContent>
      </Dialog>
    )
  }
}

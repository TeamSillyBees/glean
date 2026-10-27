import * as React from 'react'
import { Dialog as DialogPrimitive } from 'radix-ui'
import { X } from 'lucide-react'
import { cn } from '../../lib/utils'
export const Dialog = DialogPrimitive.Root
export const DialogTrigger = DialogPrimitive.Trigger
// DialogClose 允许将关闭入口放入抽屉的固定标题区域。
export const DialogClose = DialogPrimitive.Close
export const DialogTitle = DialogPrimitive.Title
export const DialogDescription = DialogPrimitive.Description
export function DialogContent({
  className,
  children,
  drawer = false,
  showCloseButton = true,
  onOpenAutoFocus,
  onCloseAutoFocus,
  ...props
}: React.ComponentProps<typeof DialogPrimitive.Content> & {
  drawer?: boolean
  showCloseButton?: boolean
}) {
  const returnFocus = React.useRef<HTMLElement | null>(null)
  return (
    <DialogPrimitive.Portal>
      <DialogPrimitive.Overlay className="fixed inset-0 z-40 bg-foreground/35 data-[state=open]:animate-in" />
      <DialogPrimitive.Content
        className={cn(
          'fixed z-50 bg-card shadow-[0_12px_60px_-20px_rgba(20,40,30,.3)] focus:outline-none',
          drawer
            ? 'inset-y-0 right-0 w-full max-w-[740px] overflow-y-auto border-l'
            : 'left-1/2 top-1/2 w-[calc(100%-2rem)] max-w-lg -translate-x-1/2 -translate-y-1/2 rounded-lg border p-7',
          className,
        )}
        {...props}
        onOpenAutoFocus={(event) => {
          returnFocus.current = document.activeElement as HTMLElement
          onOpenAutoFocus?.(event)
        }}
        onCloseAutoFocus={(event) => {
          onCloseAutoFocus?.(event)
          if (!event.defaultPrevented && returnFocus.current?.isConnected) {
            event.preventDefault()
            returnFocus.current.focus()
          }
        }}
      >
        {children}
        {showCloseButton && (
          <DialogPrimitive.Close
            className="dialog-close absolute right-5 top-5 flex size-8 items-center justify-center rounded-full text-muted-foreground transition-colors hover:bg-secondary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            aria-label="关闭"
          >
            <X className="size-4" />
          </DialogPrimitive.Close>
        )}
      </DialogPrimitive.Content>
    </DialogPrimitive.Portal>
  )
}

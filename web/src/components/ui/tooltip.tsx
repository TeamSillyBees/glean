import { Tooltip as Primitive } from 'radix-ui'
import type { ReactNode } from 'react'
export const TooltipProvider = Primitive.Provider
export function Tooltip({ children, label }: { children: ReactNode; label: string }) {
  return (
    <Primitive.Root delayDuration={350}>
      <Primitive.Trigger asChild>{children}</Primitive.Trigger>
      <Primitive.Portal>
        <Primitive.Content
          sideOffset={6}
          className="z-[70] rounded-md bg-slate-900 px-3 py-2 text-xs text-white shadow-lg"
        >
          {label}
          <Primitive.Arrow className="fill-slate-900" />
        </Primitive.Content>
      </Primitive.Portal>
    </Primitive.Root>
  )
}

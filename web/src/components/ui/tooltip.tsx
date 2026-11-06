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
          className="z-[70] w-max max-w-[calc(100vw-2rem)] rounded-md bg-foreground px-3 py-2 text-xs leading-6 text-white shadow-lg sm:max-w-xs"
        >
          {label}
          <Primitive.Arrow className="fill-foreground" />
        </Primitive.Content>
      </Primitive.Portal>
    </Primitive.Root>
  )
}

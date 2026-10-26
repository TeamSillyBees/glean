import { Select as SelectPrimitive } from 'radix-ui'
import { Check, ChevronDown, ChevronUp } from 'lucide-react'
import { cn } from '../../lib/utils'
export interface SelectOption {
  value: string
  label: string
  count?: number
}
export function Select({
  value,
  onValueChange,
  options,
  label,
  placeholder = '全部',
  className,
  disabled,
}: {
  value: string
  onValueChange: (value: string) => void
  options: SelectOption[]
  label: string
  placeholder?: string
  className?: string
  disabled?: boolean
}) {
  return (
    <SelectPrimitive.Root
      value={value || '__all__'}
      onValueChange={(v) => onValueChange(v === '__all__' ? '' : v)}
      disabled={disabled}
    >
      <SelectPrimitive.Trigger
        aria-label={label}
        className={cn(
          'flex h-9 w-full items-center justify-between gap-2 rounded-lg border border-input bg-background px-3 text-left text-[13px] outline-none transition-colors hover:border-primary/40 focus:ring-2 focus:ring-ring/25 disabled:opacity-50 [&>span]:truncate',
          className,
        )}
      >
        <SelectPrimitive.Value placeholder={placeholder} />
        <SelectPrimitive.Icon>
          <ChevronDown className="size-3.5 text-muted-foreground" />
        </SelectPrimitive.Icon>
      </SelectPrimitive.Trigger>
      <SelectPrimitive.Portal>
        <SelectPrimitive.Content
          position="popper"
          sideOffset={5}
          className="z-[60] max-h-72 min-w-[var(--radix-select-trigger-width)] overflow-hidden rounded-xl border bg-popover p-1 shadow-xl"
        >
          <SelectPrimitive.ScrollUpButton className="flex justify-center">
            <ChevronUp className="size-4" />
          </SelectPrimitive.ScrollUpButton>
          <SelectPrimitive.Viewport>
            {[{ value: '__all__', label: placeholder }, ...options].map((option) => (
              <SelectPrimitive.Item
                key={option.value}
                value={option.value}
                className="relative flex cursor-default select-none items-center gap-3 rounded-md py-2 pl-3 pr-8 text-[13px] outline-none data-[highlighted]:bg-accent data-[highlighted]:text-accent-foreground"
              >
                <SelectPrimitive.ItemText>{option.label}</SelectPrimitive.ItemText>
                {option.count != null && (
                  <span className="ml-auto text-xs text-muted-foreground">
                    {option.count.toLocaleString('zh-CN')}
                  </span>
                )}
                <SelectPrimitive.ItemIndicator className="absolute right-2">
                  <Check className="size-3.5" />
                </SelectPrimitive.ItemIndicator>
              </SelectPrimitive.Item>
            ))}
          </SelectPrimitive.Viewport>
          <SelectPrimitive.ScrollDownButton className="flex justify-center">
            <ChevronDown className="size-4" />
          </SelectPrimitive.ScrollDownButton>
        </SelectPrimitive.Content>
      </SelectPrimitive.Portal>
    </SelectPrimitive.Root>
  )
}

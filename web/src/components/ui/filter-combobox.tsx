import { useMemo, useState } from 'react'
import { Popover, PopoverTrigger, PopoverContent } from './popover'
import { Command } from 'cmdk'
import { Check, ChevronDown, Search } from 'lucide-react'
import { cn, number } from '../../lib/utils'
import type { SelectOption } from './select'

// Exact facet values only. Searching and ordering options never changes their classification.
export function FilterCombobox({
  label,
  value,
  options,
  onValueChange,
  placeholder = '全部',
  filterKey,
  disabled = false,
}: {
  label: string
  value: string
  options: SelectOption[]
  onValueChange: (value: string) => void
  placeholder?: string
  filterKey?: string
  disabled?: boolean
}) {
  const [open, setOpen] = useState(false)
  const [search, setSearch] = useState('')
  const selected = options.find((option) => option.value === value)
  const matches = useMemo(() => {
    const needle = search.trim().toLocaleLowerCase()
    return options.filter(
      (option) => !needle || `${option.label} ${option.value}`.toLocaleLowerCase().includes(needle),
    )
  }, [options, search])
  const visible = matches.slice(0, 100)
  const choose = (next: string) => {
    onValueChange(next)
    setOpen(false)
    setSearch('')
  }
  return (
    <Popover
      open={open}
      onOpenChange={(next) => {
        setOpen(next)
        if (!next) setSearch('')
      }}
    >
      <PopoverTrigger asChild>
        <button
          type="button"
          disabled={disabled}
          role="combobox"
          aria-label={label}
          aria-expanded={open}
          data-filter-key={filterKey}
          className={cn(
            'flex min-h-9 w-full items-center justify-between gap-2 rounded-md border bg-white px-3 py-2 text-left text-xs outline-none transition-colors hover:border-primary/40 focus-visible:ring-2 focus-visible:ring-ring disabled:cursor-default disabled:opacity-60',
            value && 'border-primary/40 bg-accent text-accent-foreground',
          )}
        >
          <span className="truncate">{selected?.label || value || placeholder}</span>
          <ChevronDown className="size-3.5 shrink-0 text-muted-foreground" />
        </button>
      </PopoverTrigger>
      <PopoverContent
        aria-label={`${label}筛选`}
        sideOffset={6}
        align="start"
        className="z-[70] w-[min(360px,calc(100vw-32px))] min-w-[var(--radix-popover-trigger-width)] p-0 overflow-hidden rounded-lg border bg-white shadow-xl"
      >
        <Command label={`查找${label}选项`} shouldFilter={false} loop>
          <div className="flex items-center gap-2 border-b px-3">
            <Search className="size-4 shrink-0 text-muted-foreground" />
            <Command.Input
              aria-label={`查找${label}选项`}
              value={search}
              onValueChange={setSearch}
              placeholder={`查找${label}…`}
              className="h-11 min-w-0 flex-1 bg-transparent text-sm outline-none"
            />
          </div>
          <Command.List label={`${label}选项`} className="max-h-[290px] overflow-y-auto p-1.5">
            <Command.Empty className="px-3 py-8 text-center text-xs text-muted-foreground">
              没有匹配的已记录标签
            </Command.Empty>
            {!search && (
              <Command.Item
                value="__clear__"
                onSelect={() => choose('')}
                className="flex cursor-pointer items-center rounded-md px-3 py-2.5 text-xs outline-none data-[selected=true]:bg-accent"
              >
                <span>全部</span>
                {!value && <Check className="ml-auto size-3.5 text-primary" />}
              </Command.Item>
            )}
            {visible.map((option) => (
              <Command.Item
                key={option.value}
                aria-label={
                  option.count == null
                    ? option.label
                    : `${option.label}，${number(option.count)} 篇`
                }
                value={option.value}
                onSelect={() => choose(option.value)}
                className="flex cursor-pointer items-start gap-3 rounded-md px-3 py-2.5 text-xs outline-none data-[selected=true]:bg-accent"
              >
                <span className="min-w-0 flex-1 break-words leading-5">{option.label}</span>
                {option.count != null && (
                  <span className="pt-0.5 text-xs tabular-nums text-muted-foreground">
                    {number(option.count)}
                  </span>
                )}
                {value === option.value && (
                  <Check className="mt-0.5 size-3.5 shrink-0 text-primary" />
                )}
              </Command.Item>
            ))}
          </Command.List>
        </Command>
        <div className="border-t bg-secondary/60 px-3 py-2 text-xs leading-5 text-muted-foreground">
          {matches.length > 100
            ? `显示前 100 项，输入关键词查找其余 ${number(matches.length - 100)} 项。`
            : `${number(matches.length)} 个精确标签 · 数量为当前主题内条目数`}
        </div>
      </PopoverContent>
    </Popover>
  )
}

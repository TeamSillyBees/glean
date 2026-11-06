import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, within } from '@testing-library/react'
import { expect, it, vi } from 'vitest'
import { paper } from '../test/fixtures'
import { FreshnessStrip } from './freshness-strip'
import { PaperCard } from './papers'
import { TooltipProvider } from './ui/tooltip'

it('opens source progress and paper details without implicitly submitting a surrounding form', () => {
  const client = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity } } })
  client.setQueryData(['sources'], { items: [] })
  const openSources = vi.fn()
  const openPaper = vi.fn()
  const submitted = vi.fn((event) => event.preventDefault())
  // 放入表单以暴露浏览器的默认提交行为；普通导航不能依赖父级恰好没有表单。
  render(
    <QueryClientProvider client={client}>
      <TooltipProvider>
        <form onSubmit={submitted}>
          <FreshnessStrip topic="biomed-ai" onOpen={openSources} />
          <PaperCard paper={paper} topic="biomed-ai" onOpen={openPaper} />
        </form>
      </TooltipProvider>
    </QueryClientProvider>,
  )
  fireEvent.click(screen.getByRole('button', { name: '查看来源进度' }))
  for (const button of within(screen.getByTestId('paper-card')).getAllByRole('button')) {
    fireEvent.click(button)
  }
  expect(openSources).toHaveBeenCalledOnce()
  expect(openPaper).toHaveBeenCalledTimes(2)
  expect(submitted).not.toHaveBeenCalled()
  client.clear()
})

import { fireEvent, render, screen } from '@testing-library/react'
import { expect, it, vi } from 'vitest'
import { Button } from './button'

it('only explicitly submitted buttons submit a surrounding form', () => {
  const submitted = vi.fn((event) => event.preventDefault())
  render(
    <form onSubmit={submitted}>
      <Button>刷新</Button>
      <Button type="submit">保存</Button>
    </form>,
  )
  fireEvent.click(screen.getByRole('button', { name: '刷新' }))
  expect(submitted).not.toHaveBeenCalled()
  fireEvent.click(screen.getByRole('button', { name: '保存' }))
  expect(submitted).toHaveBeenCalledOnce()
})

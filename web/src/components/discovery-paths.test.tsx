import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { DiscoveryPaths } from './discovery-paths'
describe('observation discovery paths', () => {
  it('renders string run IDs, provenance and truncation without interpreting markup', () => {
    render(
      <DiscoveryPaths
        hasMore
        items={[
          {
            runId: '9007199254740993',
            queryId: 'medical',
            queryVersion: 'q-v1',
            planVersion: 'p-v1',
            source: 'pubmed',
            mode: 'boolean',
            query: '<script>test</script>',
            discoveredAt: '2026-11-01T00:00:00Z',
          },
        ]}
      />,
    )
    expect(screen.getByRole('region', { name: '检索发现路径' })).toBeVisible()
    expect(screen.getByText(/运行 9007199254740993/)).toBeVisible()
    expect(screen.getByText(/最近 100 条/)).toBeVisible()
    expect(screen.getByText('<script>test</script>')).toBeVisible()
    expect(document.querySelector('script')).toBeNull()
  })
  it('gracefully omits absent legacy discovery data', () => {
    render(<DiscoveryPaths />)
    expect(screen.queryByRole('region', { name: '检索发现路径' })).not.toBeInTheDocument()
  })
})

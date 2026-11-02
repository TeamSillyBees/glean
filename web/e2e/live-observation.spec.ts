import { readFileSync } from 'node:fs'
import { expect, test, type Page, type Response } from '@playwright/test'
import type { Paper, PaperPage } from '../src/lib/types'

interface CommittedObservation {
  paperId: string
  observationId: string
  source: string
  sourceId: string
  title: string
  catalogTitle?: string
  sourceUrl: string
  runId: string
  llmIsMock: boolean
  scientificReviewed: boolean
  pipelineStatus: string
  pipelineStages: { name: string; status: string; attempts: number }[]
}

function response(page: Page, path: string, query: Record<string, string> = {}) {
  return page.waitForResponse((item) => {
    const url = new URL(item.url())
    return (
      item.request().method() === 'GET' &&
      url.pathname === path &&
      Object.entries(query).every(([key, value]) => url.searchParams.get(key) === value)
    )
  })
}

async function data<T>(item: Response): Promise<T> {
  expect(item.status()).toBe(200)
  const body = await item.json()
  expect(body.code).toBe('OK')
  return body.data as T
}

test('one official EuropePMC observation reaches real PostgreSQL, API and browser with model processing explicitly disabled', async ({
  page,
}, info) => {
  const reportPath = process.env.GLEAN_E2E_LIVE_REPORT
  const password = process.env.GLEAN_E2E_PASSWORD
  if (!reportPath || !password)
    throw new Error('Live observation report and generated CI password are required')
  const report = JSON.parse(readFileSync(reportPath, 'utf8')) as {
    observations: CommittedObservation[]
  }
  expect(report.observations.length).toBeGreaterThan(0)
  const committed = report.observations[0]
  expect(committed.source).toBe('europepmc')
  for (const id of [committed.paperId, committed.observationId, committed.runId]) {
    expect(id).toMatch(/^[1-9][0-9]*$/)
  }
  expect(committed.sourceId).not.toBe('')
  expect(committed.llmIsMock).toBe(false)
  expect(committed.pipelineStatus).toBe('awaiting_configuration')
  expect(committed.pipelineStages.length).toBeGreaterThan(0)
  expect(committed.pipelineStages.every((stage) => stage.attempts === 0)).toBe(true)
  expect(committed.pipelineStages.find((stage) => stage.name === 'material')?.status).toBe(
    'awaiting_configuration',
  )
  expect(committed.scientificReviewed).toBe(false)
  const origin = new URL(committed.sourceUrl)
  expect(origin.protocol).toBe('https:')
  expect(origin.hostname).toBe('www.ebi.ac.uk')
  expect(origin.username).toBe('')
  expect(origin.password).toBe('')
  const title = committed.catalogTitle || committed.title
  expect(title).not.toBe('')
  const errors: string[] = []
  page.on('pageerror', (error) => errors.push(error.message))

  await page.goto('/')
  await page
    .getByLabel('用户名', { exact: true })
    .fill(process.env.GLEAN_E2E_USERNAME || 'glean-browser')
  await page.getByLabel('密码', { exact: true }).fill(password)
  const initial = response(page, '/api/v1/catalog/papers')
  await page.getByRole('button', { name: '进入工作台', exact: true }).click()
  await data<PaperPage>(await initial)

  const all = response(page, '/api/v1/catalog/papers', { collectionScope: 'all' })
  await page
    .getByRole('group', { name: '目录范围', exact: true })
    .getByRole('button', { name: '全部记录', exact: true })
    .click()
  await data<PaperPage>(await all)
  await page.getByRole('textbox', { name: '搜索论文或来源', exact: true }).fill(title)
  const searched = response(page, '/api/v1/catalog/papers', { q: title, collectionScope: 'all' })
  await page.getByRole('button', { name: '搜索', exact: true }).click()
  const found = await data<PaperPage>(await searched)
  const paper = found.items.find((item) => item.id === committed.paperId)
  expect(
    paper,
    'the actual committed live observation must be discoverable through real catalog HTTP',
  ).toBeDefined()
  const detailResponse = response(page, `/api/v1/catalog/papers/${committed.paperId}`)
  await page
    .locator(`[data-testid="paper-card"][data-paper-id="${committed.paperId}"]`)
    .getByRole('button', { name: `查看 ${paper!.titleZh || paper!.title} 的详情`, exact: true })
    .click()
  const detail = await data<Paper>(await detailResponse)
  expect(detail.isMock).toBe(false)
  expect(detail.observationCount).toBeGreaterThan(0)
  const observed = detail.observations?.find((item) => item.id === committed.observationId)
  expect(observed).toBeDefined()
  expect(observed!.source).toBe('europepmc')
  expect(observed!.sourceId).toBe(committed.sourceId)
  expect(observed!.sourceUrl).toBe(committed.sourceUrl)
  expect(observed!.runId).toBe(committed.runId)
  expect(observed!.review).toMatchObject({
    isMock: false,
    scientificReviewed: false,
    inclusionStatus: 'pending-review',
    pipeline: { status: 'awaiting_configuration' },
  })
  expect(observed!.review!.pipeline!.stages.every((stage) => stage.attempts === 0)).toBe(true)
  expect(Object.keys(observed!.raw).length).toBeGreaterThan(0)
  expect(observed!.title).not.toBe('')

  const drawer = page.getByRole('dialog', { name: '论文详情', exact: true })
  await expect(drawer.getByText('缺少模型配置', { exact: true }).first()).toBeVisible()
  await drawer.getByRole('tab', { name: '来源与证据', exact: true }).click()
  const version = drawer.locator(
    `[data-testid="source-observation"][data-observation-id="${committed.observationId}"]`,
  )
  await version.locator(':scope > summary').click()
  await expect(version).toHaveAttribute('open', '')
  await expect(version.getByText(observed!.title, { exact: true })).toBeVisible()
  await expect(
    version.getByText('此版本的模型阅读为模拟输出；来源内容是真实采集记录。', { exact: true }),
  ).toHaveCount(0)
  await expect(version.getByRole('link', { name: '官方来源记录', exact: true })).toHaveAttribute(
    'href',
    committed.sourceUrl,
  )
  await version.scrollIntoViewIfNeeded()
  expect(
    await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth + 1),
  ).toBe(true)
  await page.screenshot({
    path: info.outputPath(`${info.project.name}-live-europepmc-viewport.png`),
  })
  await page.screenshot({
    path: info.outputPath(`${info.project.name}-live-europepmc.png`),
    fullPage: true,
  })
  expect(errors).toEqual([])
})

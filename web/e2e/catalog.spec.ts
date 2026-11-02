import { expect, test, type Page, type Response, type TestInfo } from '@playwright/test'
import type { Facets, Paper, PaperPage, Topic, Venue } from '../src/lib/types'

// No page.route, synthetic papers, token injection or mocked HTTP: every assertion
// below observes the built UI talking to the real Go API and isolated PostgreSQL.
function apiResponse(page: Page, path: string, query: Record<string, string> = {}) {
  return page.waitForResponse((response) => {
    const url = new URL(response.url())
    return (
      url.pathname === path &&
      response.request().method() === 'GET' &&
      Object.entries(query).every(([key, value]) => url.searchParams.get(key) === value)
    )
  })
}

function catalogResponse(page: Page, suffix: string, query: Record<string, string> = {}) {
  return apiResponse(page, `/api/v1/catalog/${suffix}`, query)
}

async function data<T>(response: Response): Promise<T> {
  expect(response.status()).toBe(200)
  const envelope = await response.json()
  expect(envelope.code).toBe('OK')
  return envelope.data as T
}

async function login(page: Page, username: string) {
  const password = process.env.GLEAN_E2E_PASSWORD
  if (!password || password.length < 24)
    throw new Error('Generated CI-only browser password is required')
  await page.getByLabel('用户名', { exact: true }).fill(username)
  await page.getByLabel('密码', { exact: true }).fill(password)
  const papers = catalogResponse(page, 'papers')
  await page.getByRole('button', { name: '进入工作台', exact: true }).click()
  const result = await data<PaperPage>(await papers)
  await expect(page.getByTestId('paper-card').first()).toBeVisible()
  return result
}

async function navigate(page: Page, name: string) {
  const mobileToggle = page.getByRole('button', { name: '打开导航', exact: true })
  if (await mobileToggle.isVisible()) await mobileToggle.click()
  const dialog = page.getByRole('dialog', { name: '导航', exact: true })
  const scope = (await dialog.isVisible()) ? dialog : page.locator('.desktop-sidebar')
  await scope.getByRole('button', { name, exact: true }).click()
  await expect(dialog).toBeHidden()
}

async function search(page: Page, query: string) {
  await page.getByRole('textbox', { name: '搜索论文或来源' }).fill(query)
  const response = catalogResponse(page, 'papers', { q: query })
  await page.getByRole('button', { name: '搜索', exact: true }).click()
  return data<PaperPage>(await response)
}

async function collectionScope(page: Page, label: string, scope: string) {
  const papers = catalogResponse(page, 'papers', { collectionScope: scope })
  const facets = catalogResponse(page, 'facets', { collectionScope: scope })
  const button = page
    .getByRole('group', { name: '目录范围', exact: true })
    .getByRole('button', { name: label, exact: true })
  await button.click()
  const result = await data<PaperPage>(await papers)
  await data<Facets>(await facets)
  await expect(button).toHaveAttribute('aria-pressed', 'true')
  await expect(page.getByTestId('filters-ready').first()).toBeAttached()
  return result
}

async function detail(page: Page, paper: Paper) {
  const response = catalogResponse(page, `papers/${paper.id}`)
  await page
    .locator(`[data-testid="paper-card"][data-paper-id="${paper.id}"]`)
    .getByRole('button', { name: `查看 ${paper.titleZh || paper.title} 的详情`, exact: true })
    .click()
  const result = await data<Paper>(await response)
  await expect(page.getByRole('dialog', { name: '论文详情', exact: true })).toBeVisible()
  return result
}

async function updateState(page: Page, id: string, button: string) {
  const update = page.waitForResponse(
    (response) =>
      new URL(response.url()).pathname === `/api/v1/catalog/papers/${id}/state` &&
      response.request().method() === 'PUT',
  )
  const drawer = page.getByRole('dialog', { name: '论文详情', exact: true })
  await drawer.getByRole('button', { name: button, exact: true }).click()
  // The authoritative PUT response is merged into the UI cache. A redundant GET
  // is not part of the contract; normal close/reopen and reload below verify DB state.
  const state = await data<Pick<Paper, 'hidden' | 'readStatus'>>(await update)
  const readLabels = { unread: '未读', reading: '在读', read: '已读' }
  const selectedRead = drawer.getByRole('button', {
    name: readLabels[state.readStatus],
    exact: true,
  })
  await expect(selectedRead.locator('svg.lucide-check')).toBeVisible()
  await expect(selectedRead).toBeEnabled()
  await expect(
    drawer.getByRole('button', { name: state.hidden ? '恢复显示' : '隐藏', exact: true }),
  ).toBeVisible()
  return state
}

async function screenshot(page: Page, info: TestInfo, name: string) {
  await page.screenshot({ path: info.outputPath(`${info.project.name}-${name}-viewport.png`) })
  await page.screenshot({
    path: info.outputPath(`${info.project.name}-${name}.png`),
    fullPage: true,
  })
}

async function noHorizontalOverflow(page: Page) {
  expect(
    await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth + 1),
  ).toBe(true)
}

// Resize the actual authenticated application, never a rendered fixture. Keep
// this in the historical phase: no public provider uptime is involved.
async function responsiveCatalog(page: Page, info: TestInfo) {
  if (info.project.name !== 'desktop') return
  const original = page.viewportSize()!
  try {
    for (const width of [320, 375, 768, 1024, 1280, 1440, 1920]) {
      await page.setViewportSize({ width, height: 1000 })
      await page.evaluate(() => window.scrollTo(0, 0))
      await noHorizontalOverflow(page)
      const search = page.getByRole('textbox', { name: '搜索论文或来源' })
      const paper = page.getByTestId('paper-card').first()
      for (const element of [search, paper]) {
        await expect(element).toBeVisible()
        const box = await element.boundingBox()
        expect(box).not.toBeNull()
        expect(box!.x).toBeGreaterThanOrEqual(0)
        expect(box!.x + box!.width).toBeLessThanOrEqual(width + 1)
      }
      await expect(page.getByRole('button', { name: '搜索', exact: true })).toBeVisible()
      await page.screenshot({ path: info.outputPath(`desktop-catalog-${width}-viewport.png`) })
    }
  } finally {
    await page.setViewportSize(original)
    await page.evaluate(() => window.scrollTo(0, 0))
  }
}

test('real historical catalog: login, filters, detail, durable personal state and account isolation', async ({
  page,
  browser,
}, info) => {
  const primary = process.env.GLEAN_E2E_USERNAME || 'glean-browser'
  const secondary = process.env.GLEAN_E2E_SECOND_USERNAME || 'glean-browser-other'
  const username = info.project.name === 'mobile' ? secondary : primary
  const otherUsername = username === primary ? secondary : primary
  const errors: string[] = []
  const detailChunks: string[] = []
  page.on('request', (request) => {
    if (/\/assets\/paper-detail-body-[^/]+\.js$/.test(new URL(request.url()).pathname))
      detailChunks.push(request.url())
  })
  page.on('pageerror', (error) => errors.push(error.message))
  await page.goto('/')
  await screenshot(page, info, 'login')
  const topicsResponse = catalogResponse(page, 'topics')
  const initialFacetsResponse = catalogResponse(page, 'facets', { collectionScope: 'curated' })
  const initial = await login(page, username)
  await data<Facets>(await initialFacetsResponse)
  await expect(page.getByTestId('filters-ready').first()).toBeAttached()
  const topics = await data<{ items: (Topic & { curatedCount: number })[] }>(await topicsResponse)
  expect(
    Object.fromEntries(topics.items.map((topic) => [topic.slug, topic.membershipCount])),
  ).toEqual({
    'biomed-ai': 31399,
    'llm-agent': 4059,
    'biomed-worldmodel': 4037,
  })
  expect(Object.fromEntries(topics.items.map((topic) => [topic.slug, topic.curatedCount]))).toEqual(
    {
      'biomed-ai': 8556,
      'llm-agent': 1204,
      'biomed-worldmodel': 1210,
    },
  )
  expect(initial.total).toBe(8556)
  await expect(
    page
      .getByRole('group', { name: '目录范围', exact: true })
      .getByRole('button', { name: '精选目录', exact: true }),
  ).toHaveAttribute('aria-pressed', 'true')
  expect(initial.items.every((paper) => !paper.isMock)).toBe(true)
  await expect(page.getByTestId('results-count')).toContainText(
    initial.total.toLocaleString('zh-CN'),
  )
  await noHorizontalOverflow(page)
  await screenshot(page, info, 'catalog')
  await responsiveCatalog(page, info)

  expect((await collectionScope(page, '全部记录', 'all')).total).toBe(31399)
  await screenshot(page, info, 'all-records')
  const pending = await collectionScope(page, '待审核', 'pending')
  expect(pending.total).toBe(0)
  expect(pending.items).toEqual([])
  await expect(page.getByTestId('paper-card')).toHaveCount(0)
  await screenshot(page, info, 'pending-empty')
  // Returning to a recently fetched scope may use React Query's real-response
  // cache. Do not demand a redundant HTTP call or weaken the application cache.
  const curatedButton = page
    .getByRole('group', { name: '目录范围', exact: true })
    .getByRole('button', { name: '精选目录', exact: true })
  await curatedButton.click()
  await expect(curatedButton).toHaveAttribute('aria-pressed', 'true')
  await expect(page.getByTestId('results-count')).toContainText('8,556')
  await expect(page.getByTestId('filters-ready').first()).toBeAttached()

  const year = String(initial.items.find((paper) => paper.year > 0)!.year)
  const openFilters = page.getByRole('button', { name: /^筛选/ })
  if (await openFilters.isVisible()) await openFilters.click()
  const yearResponse = catalogResponse(page, 'papers', { year })
  await page
    .getByRole('combobox', { name: '发表年份', exact: true })
    .filter({ visible: true })
    .click()
  const yearPicker = page.getByRole('dialog', { name: '发表年份筛选', exact: true })
  await expect(yearPicker).toBeVisible()
  await yearPicker.getByRole('combobox', { name: '查找发表年份选项', exact: true }).fill(year)
  await page.getByRole('option', { name: new RegExp(`^${year}(?:[^0-9]|$)`) }).click()
  await expect(yearPicker).toBeHidden()
  const filtered = await data<PaperPage>(await yearResponse)
  expect(filtered.items.length).toBeGreaterThan(0)
  expect(filtered.items.every((paper) => paper.year === Number(year))).toBe(true)
  const filtersDialog = page.getByRole('dialog', { name: '筛选论文', exact: true })
  if (await filtersDialog.isVisible())
    await filtersDialog.getByRole('button', { name: '查看结果', exact: true }).click()
  await expect(filtersDialog).toBeHidden()
  if (await openFilters.isVisible()) {
    // Repeated real dialog dismissals must restore focus and never leave a blocking overlay.
    for (let attempt = 0; attempt < 2; attempt += 1) {
      await openFilters.click()
      await expect(filtersDialog).toBeVisible()
      await filtersDialog.getByRole('button', { name: '关闭', exact: true }).click()
      await expect(filtersDialog).toBeHidden()
      await expect(openFilters).toBeFocused()
    }
  }

  const target = filtered.items[0]
  const query = target.doi || target.title
  const empty = await search(page, '__glean_browser_no_matching_historical_record_39495__')
  expect(empty.total).toBe(0)
  expect(empty.items).toEqual([])
  await expect(page.getByText('暂时没有匹配的论文', { exact: true })).toBeVisible()
  await expect(page.getByTestId('paper-card')).toHaveCount(0)
  const found = await search(page, query)
  expect(found.items.some((paper) => paper.id === target.id)).toBe(true)
  // Observe real production chunk requests without intercepting any API or asset.
  expect(detailChunks).toHaveLength(0)
  const original = await detail(page, target)
  expect(detailChunks).toHaveLength(1)
  expect(original.isMock).toBe(false)
  expect(original.memberships.length).toBeGreaterThan(0)
  const drawer = page.getByRole('dialog', { name: '论文详情', exact: true })
  await expect(
    drawer.getByRole('heading', { name: target.titleZh || target.title, exact: true }),
  ).toBeVisible()
  // Only the string paper ID is shareable; private search/filter state stays in memory.
  const sharedLink = new URL(page.url())
  expect([...sharedLink.searchParams.entries()]).toEqual([['paper', target.id]])
  expect(sharedLink.hash).toBe('')
  await page.context().grantPermissions(['clipboard-read', 'clipboard-write'])
  await drawer.getByRole('button', { name: '复制论文链接', exact: true }).click()
  await expect.poll(() => page.evaluate(() => navigator.clipboard.readText())).toBe(sharedLink.href)
  await page.goBack()
  await expect(drawer).toBeHidden()
  expect(new URL(page.url()).searchParams.has('paper')).toBe(false)
  await expect(page.getByRole('textbox', { name: '搜索论文或来源' })).toHaveValue(query)
  await expect(page.getByTestId('paper-card')).toHaveCount(found.items.length)
  await page.goForward()
  await expect(drawer).toBeVisible()
  await expect(
    drawer.getByRole('heading', { name: target.titleZh || target.title, exact: true }),
  ).toBeVisible()
  expect(new URL(page.url()).searchParams.get('paper')).toBe(target.id)
  await noHorizontalOverflow(page)
  await screenshot(page, info, 'paper-detail')
  const detailTrigger = page
    .locator(`[data-testid="paper-card"][data-paper-id="${target.id}"]`)
    .getByRole('button', { name: `查看 ${target.titleZh || target.title} 的详情`, exact: true })
  const closeDetail = drawer.getByRole('button', { name: '关闭', exact: true })
  // A close button must be pointer-reachable, not just present in the DOM. Check
  // both the initial and scrolled sticky header, then reopen through the real UI.
  for (const scrollToEnd of [false, true]) {
    if (scrollToEnd) {
      await drawer.evaluate((element) => element.scrollTo(0, element.scrollHeight))
      await expect.poll(() => drawer.evaluate((element) => element.scrollTop)).toBeGreaterThan(0)
      await screenshot(page, info, 'paper-detail-scrolled')
    }
    await expect(closeDetail).toHaveCount(1)
    await expect(closeDetail).toBeInViewport()
    await closeDetail.click()
    await expect(drawer).toBeHidden()
    await expect(detailTrigger).toBeFocused()
    await detail(page, target)
  }
  await drawer.getByRole('tab', { name: '来源与证据', exact: true }).click()
  expect(original.origin).toBe('historical')
  expect(original.observationCount ?? 0).toBe(0)
  await expect(
    drawer.getByText('暂无新增来源版本。下方仍保留导入目录中的证据记录。', { exact: true }),
  ).toBeVisible()
  for (const link of await drawer
    .locator('a[href]')
    .evaluateAll((links) => links.map((element) => (element as HTMLAnchorElement).href))) {
    const destination = new URL(link)
    expect(['https:', 'http:']).toContain(destination.protocol)
    expect(destination.username).toBe('')
    expect(destination.password).toBe('')
  }
  await screenshot(page, info, 'source-evidence')
  const read = await updateState(page, target.id, '已读')
  expect(read.readStatus).toBe('read')
  const hidden = await updateState(page, target.id, '隐藏')
  expect(hidden.hidden).toBe(true)
  expect(hidden.readStatus).toBe('read')
  await expect(
    page.locator(`[data-testid="paper-card"][data-paper-id="${target.id}"]`),
  ).toBeHidden()
  await drawer.getByRole('button', { name: '关闭', exact: true }).click()
  await expect(drawer).toBeHidden()
  await expect(page.getByRole('textbox', { name: '搜索论文或来源' })).toBeFocused()
  // A copied link survives a fresh login/reload and reads hidden personal state
  // without automatically restoring it. No list filter is needed to reach it.
  const linkedPage = await page.context().newPage()
  try {
    await linkedPage.goto(sharedLink.href)
    expect(new URL(linkedPage.url()).searchParams.get('paper')).toBe(target.id)
    const hiddenDetail = catalogResponse(linkedPage, `papers/${target.id}`)
    await login(linkedPage, username)
    const linkedHidden = await data<Paper>(await hiddenDetail)
    expect(linkedHidden.hidden).toBe(true)
    expect(linkedHidden.readStatus).toBe('read')
    const linkedDrawer = linkedPage.getByRole('dialog', { name: '论文详情', exact: true })
    await expect(linkedDrawer.getByRole('button', { name: '恢复显示', exact: true })).toBeVisible()
    await linkedPage.reload()
    await expect(linkedPage.getByLabel('用户名', { exact: true })).toBeVisible()
    expect(new URL(linkedPage.url()).searchParams.get('paper')).toBe(target.id)
    const reloadedDetail = catalogResponse(linkedPage, `papers/${target.id}`)
    await login(linkedPage, username)
    expect((await data<Paper>(await reloadedDetail)).hidden).toBe(true)
    await expect(linkedDrawer.getByRole('button', { name: '恢复显示', exact: true })).toBeVisible()
    const historyLength = await linkedPage.evaluate(() => history.length)
    await linkedDrawer.getByRole('button', { name: '关闭', exact: true }).click()
    await expect(linkedDrawer).toBeHidden()
    await expect(linkedPage).toHaveURL(new URL(sharedLink.pathname, sharedLink.origin).href)
    expect(await linkedPage.evaluate(() => history.length)).toBe(historyLength)
    await expect(linkedPage.getByTestId('paper-card').first()).toBeVisible()
  } finally {
    await linkedPage.close()
  }
  const hiddenList = catalogResponse(page, 'papers', { hidden: 'only' })
  await navigate(page, '已隐藏')
  expect(
    (await data<PaperPage>(await hiddenList)).items.some((paper) => paper.id === target.id),
  ).toBe(true)
  const persistedHidden = await detail(page, target)
  expect(persistedHidden.hidden).toBe(true)
  expect(persistedHidden.readStatus).toBe('read')
  expect(persistedHidden.memberships).toEqual(original.memberships)
  const restored = await updateState(page, target.id, '恢复显示')
  expect(restored.hidden).toBe(false)
  expect(restored.readStatus).toBe('read')
  await expect(
    page.locator(`[data-testid="paper-card"][data-paper-id="${target.id}"]`),
  ).toBeHidden()
  await drawer.getByRole('button', { name: '关闭', exact: true }).click()
  await expect(drawer).toBeHidden()
  await expect(page.getByRole('textbox', { name: '搜索论文或来源' })).toBeFocused()

  // Tokens are memory-only: a reload requires real login, but PostgreSQL keeps read state.
  await page.reload()
  await expect(page.getByLabel('用户名', { exact: true })).toBeVisible()
  expect(await page.evaluate(() => [localStorage.length, sessionStorage.length])).toEqual([0, 0])
  await login(page, username)
  await search(page, query)
  const persistedRead = await detail(page, target)
  expect(persistedRead.readStatus).toBe('read')
  expect(persistedRead.hidden).toBe(false)
  expect(persistedRead.memberships).toEqual(original.memberships)

  // A separate browser context/account must not inherit either personal flag.
  const otherContext = await browser.newContext({
    baseURL: process.env.GLEAN_E2E_BASE_URL || 'http://127.0.0.1:18081',
  })
  try {
    const other = await otherContext.newPage()
    // A preceding page must not be visited by closing a directly landed link.
    await other.goto('/?landing=before')
    await other.goto(sharedLink.href)
    const otherDetail = catalogResponse(other, `papers/${target.id}`)
    await login(other, otherUsername)
    const otherPaper = await data<Paper>(await otherDetail)
    expect(otherPaper.readStatus).toBe('unread')
    expect(otherPaper.hidden).toBe(false)
    const otherDrawer = other.getByRole('dialog', { name: '论文详情', exact: true })
    await expect(
      otherDrawer.getByRole('heading', { name: target.titleZh || target.title, exact: true }),
    ).toBeVisible()
    await otherDrawer.getByRole('button', { name: '关闭', exact: true }).click()
    await expect(otherDrawer).toBeHidden()
    await expect(other).toHaveURL(new URL(sharedLink.pathname, sharedLink.origin).href)
    await expect(other.getByTestId('paper-card').first()).toBeVisible()
    // A valid string ID beyond the seeded catalog must reach the real 404 path.
    const missingId = '9223372036854775807'
    await other.goto(`/?paper=${missingId}`)
    const missingDetail = catalogResponse(other, `papers/${missingId}`)
    await login(other, otherUsername)
    expect((await missingDetail).status()).toBe(404)
    await expect(otherDrawer.getByText('未找到这篇论文', { exact: true })).toBeVisible()
    await otherDrawer.getByRole('button', { name: '返回列表', exact: true }).click()
    await expect(otherDrawer).toBeHidden()
    await expect(other).toHaveURL(new URL(sharedLink.pathname, sharedLink.origin).href)
  } finally {
    await otherContext.close()
  }
  // Leave the shared real dataset clean for the second viewport project.
  expect((await updateState(page, target.id, '未读')).readStatus).toBe('unread')
  await drawer.getByRole('button', { name: '关闭', exact: true }).click()

  // A configured real provider is not a completed harvest. No scheduler or live
  // canary runs in this isolated historical-only database, so progress stays unknown.
  await navigate(page, '来源健康')
  await expect(page.getByText('真实来源采集', { exact: true })).toBeVisible()
  await expect(page.getByText('定时计划未启用', { exact: true })).toBeVisible()
  const sourcesResponse = apiResponse(page, '/api/v1/monitor/sources')
  await page.getByRole('button', { name: '刷新状态', exact: true }).click()
  const sources = await data<{
    items: { source: string; acquiredThrough: string; enabled: boolean }[]
    schedule: {
      acquisitionMode: string
      reviewMode: string
      schedulerEnabled: boolean
      completedRuns: number
    }
  }>(await sourcesResponse)
  expect(sources.schedule).toMatchObject({
    acquisitionMode: 'live',
    reviewMode: 'mock',
    schedulerEnabled: false,
    completedRuns: 0,
  })
  expect(sources.items.map((item) => item.source).sort()).toEqual([
    'arxiv',
    'biorxiv',
    'crossref',
    'europepmc',
    'medrxiv',
    'pubmed',
  ])
  expect(
    sources.items.every(
      (item) => !item.acquiredThrough || item.acquiredThrough.startsWith('0001-'),
    ),
  ).toBe(true)
  await expect(page.getByText('尚无完成记录', { exact: true })).toHaveCount(6)
  await noHorizontalOverflow(page)
  await screenshot(page, info, 'source-health')

  // This historical-only database has no fabricated quarantined records. Exercise
  // the real read-only endpoint, empty state, source filter and repeated navigation.
  const quarantineResponse = apiResponse(page, '/api/v1/monitor/quarantine', {
    page: '1',
    pageSize: '20',
  })
  await navigate(page, '隔离记录')
  expect(
    await data<{ items: unknown[]; total: number; page: number; pageSize: number }>(
      await quarantineResponse,
    ),
  ).toEqual({ items: [], total: 0, page: 1, pageSize: 20 })
  await expect(page.getByText('没有匹配的隔离记录', { exact: true })).toBeVisible()
  await expect(page.getByText('隔离不等于论文撤稿或科学结论有误。', { exact: false })).toBeVisible()
  const quarantinePages = page.getByRole('navigation', { name: '隔离记录分页', exact: true })
  await expect(quarantinePages.getByRole('button', { name: '上一页', exact: true })).toBeDisabled()
  await expect(quarantinePages.getByRole('button', { name: '下一页', exact: true })).toBeDisabled()
  await page.getByRole('combobox', { name: '隔离记录来源', exact: true }).click()
  const arxivQuarantine = apiResponse(page, '/api/v1/monitor/quarantine', {
    page: '1',
    source: 'arxiv',
  })
  await page.getByRole('option', { name: 'arxiv', exact: true }).click()
  expect((await data<{ items: unknown[]; total: number }>(await arxivQuarantine)).items).toEqual([])
  await expect(page.getByRole('combobox', { name: '隔离记录来源', exact: true })).toHaveText(
    'arxiv',
  )
  await navigate(page, '来源健康')
  await expect(page.getByText('尚无完成记录', { exact: true })).toHaveCount(6)
  await navigate(page, '隔离记录')
  await expect(page.getByRole('combobox', { name: '隔离记录来源', exact: true })).toHaveText(
    '全部来源',
  )
  // A recently cached view may not refetch on navigation; use its actual refresh control.
  const refreshedQuarantine = apiResponse(page, '/api/v1/monitor/quarantine', { page: '1' })
  await page.getByRole('button', { name: '刷新记录', exact: true }).click()
  const refreshed = await refreshedQuarantine
  expect(new URL(refreshed.url()).searchParams.has('source')).toBe(false)
  expect((await data<{ items: unknown[]; total: number }>(refreshed)).total).toBe(0)
  await expect(page.getByText('没有匹配的隔离记录', { exact: true })).toBeVisible()
  await noHorizontalOverflow(page)
  await screenshot(page, info, 'quarantine-empty')

  const runsResponse = apiResponse(page, '/api/v1/monitor/runs')
  await navigate(page, '扫描记录')
  expect((await data<{ items: unknown[] }>(await runsResponse)).items).toEqual([])
  await expect(page.getByText('还没有扫描记录', { exact: true })).toBeVisible()

  const venuesResponse = catalogResponse(page, 'venues')
  await navigate(page, '期刊与会议')
  const venues = await data<{ items: Venue[] }>(await venuesResponse)
  expect(venues.items.length).toBeGreaterThan(0)
  const venue = venues.items[0]
  const venueSearchResponse = catalogResponse(page, 'venues', { q: venue.name })
  await page.getByRole('textbox', { name: '搜索期刊或会议', exact: true }).fill(venue.name)
  await page.getByRole('button', { name: '搜索', exact: true }).click()
  expect(
    (await data<{ items: Venue[] }>(await venueSearchResponse)).items.some(
      (item) => item.id === venue.id,
    ),
  ).toBe(true)
  await expect(
    page.getByText(venue.name, { exact: true }).filter({ visible: true }).first(),
  ).toBeVisible()
  await screenshot(page, info, 'venues')
  await noHorizontalOverflow(page)
  expect(errors).toEqual([])
})

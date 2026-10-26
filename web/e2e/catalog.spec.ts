import { expect, test, type Page, type Response, type TestInfo } from '@playwright/test'
import type { Paper, PaperPage, Topic } from '../src/lib/types'

// No page.route, synthetic papers, token injection or mocked HTTP: every assertion
// below observes the built UI talking to the real Go API and isolated PostgreSQL.
function catalogResponse(page: Page, suffix: string, query: Record<string, string> = {}) {
  return page.waitForResponse((response) => {
    const url = new URL(response.url())
    return (
      url.pathname === `/api/v1/catalog/${suffix}` &&
      response.request().method() === 'GET' &&
      Object.entries(query).every(([key, value]) => url.searchParams.get(key) === value)
    )
  })
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
  const refreshed = catalogResponse(page, `papers/${id}`)
  await page
    .getByRole('dialog', { name: '论文详情', exact: true })
    .getByRole('button', { name: button, exact: true })
    .click()
  expect((await update).status()).toBe(200)
  return data<Paper>(await refreshed)
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

test('real historical catalog: login, filters, detail, durable personal state and account isolation', async ({
  page,
  browser,
}, info) => {
  const primary = process.env.GLEAN_E2E_USERNAME || 'glean-browser'
  const secondary = process.env.GLEAN_E2E_SECOND_USERNAME || 'glean-browser-other'
  const username = info.project.name === 'mobile' ? secondary : primary
  const otherUsername = username === primary ? secondary : primary
  const errors: string[] = []
  page.on('pageerror', (error) => errors.push(error.message))
  await page.goto('/')
  await screenshot(page, info, 'login')
  const topicsResponse = catalogResponse(page, 'topics')
  const initial = await login(page, username)
  const topics = await data<{ items: Topic[] }>(await topicsResponse)
  expect(
    Object.fromEntries(topics.items.map((topic) => [topic.slug, topic.membershipCount])),
  ).toEqual({
    'biomed-ai': 31399,
    'llm-agent': 4059,
    'biomed-worldmodel': 4037,
  })
  expect(initial.total).toBeGreaterThan(30000)
  expect(initial.items.every((paper) => !paper.isMock)).toBe(true)
  await expect(page.getByTestId('results-count')).toContainText(
    initial.total.toLocaleString('zh-CN'),
  )
  await noHorizontalOverflow(page)
  await screenshot(page, info, 'catalog')

  const year = String(initial.items.find((paper) => paper.year > 0)!.year)
  const openFilters = page.getByRole('button', { name: /^筛选/ })
  if (await openFilters.isVisible()) await openFilters.click()
  const yearResponse = catalogResponse(page, 'papers', { year })
  await page
    .getByRole('combobox', { name: '发表年份', exact: true })
    .filter({ visible: true })
    .click()
  await page.getByRole('option', { name: new RegExp(`^${year}(?:[^0-9]|$)`) }).click()
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
  const found = await search(page, query)
  expect(found.items.some((paper) => paper.id === target.id)).toBe(true)
  const original = await detail(page, target)
  expect(original.isMock).toBe(false)
  expect(original.memberships.length).toBeGreaterThan(0)
  const drawer = page.getByRole('dialog', { name: '论文详情', exact: true })
  await expect(
    drawer.getByRole('heading', { name: target.titleZh || target.title, exact: true }),
  ).toBeVisible()
  await noHorizontalOverflow(page)
  await screenshot(page, info, 'paper-detail')
  await drawer.getByRole('tab', { name: '来源与证据', exact: true }).click()
  await screenshot(page, info, 'source-evidence')
  const read = await updateState(page, target.id, '已读')
  expect(read.readStatus).toBe('read')
  expect(read.memberships).toEqual(original.memberships)
  const hidden = await updateState(page, target.id, '隐藏')
  expect(hidden.hidden).toBe(true)
  await drawer.getByRole('button', { name: '关闭', exact: true }).click()
  await expect(
    page.locator(`[data-testid="paper-card"][data-paper-id="${target.id}"]`),
  ).toBeHidden()
  const hiddenList = catalogResponse(page, 'papers', { hidden: 'only' })
  await navigate(page, '已隐藏')
  expect(
    (await data<PaperPage>(await hiddenList)).items.some((paper) => paper.id === target.id),
  ).toBe(true)
  await detail(page, target)
  expect((await updateState(page, target.id, '恢复显示')).hidden).toBe(false)
  await drawer.getByRole('button', { name: '关闭', exact: true }).click()

  // Tokens are memory-only: a reload requires real login, but PostgreSQL keeps read state.
  await page.reload()
  await expect(page.getByLabel('用户名', { exact: true })).toBeVisible()
  expect(await page.evaluate(() => [localStorage.length, sessionStorage.length])).toEqual([0, 0])
  await login(page, username)
  await search(page, query)
  expect((await detail(page, target)).readStatus).toBe('read')

  // A separate browser context/account must not inherit either personal flag.
  const otherContext = await browser.newContext({
    baseURL: process.env.GLEAN_E2E_BASE_URL || 'http://127.0.0.1:18081',
  })
  try {
    const other = await otherContext.newPage()
    await other.goto('/')
    await login(other, otherUsername)
    await search(other, query)
    const otherPaper = await detail(other, target)
    expect(otherPaper.readStatus).toBe('unread')
    expect(otherPaper.hidden).toBe(false)
  } finally {
    await otherContext.close()
  }
  // Leave the shared real dataset clean for the second viewport project.
  expect((await updateState(page, target.id, '未读')).readStatus).toBe('unread')
  await drawer.getByRole('button', { name: '关闭', exact: true }).click()
  await noHorizontalOverflow(page)
  expect(errors).toEqual([])
})

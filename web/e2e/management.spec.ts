import { expect, test, type Page, type Response } from '@playwright/test'
import type {
  CatalogHistory,
  Correction,
  Facets,
  ManualReview,
  Paper,
  PaperPage,
  Topic,
} from '../src/lib/types'

// This project runs after historical acceptance against real Go/PostgreSQL.
// Only the first access GET below is intentionally replaced with a 503; every
// subsequent request reaches the backend. No synthetic papers, token injection,
// HAR or traces. This is fault injection, not an external live-source check.
function response(page: Page, path: string, method = 'GET', query: Record<string, string> = {}) {
  return page.waitForResponse((res) => {
    const url = new URL(res.url())
    return (
      url.pathname === path &&
      res.request().method() === method &&
      Object.entries(query).every(([key, value]) => (url.searchParams.get(key) ?? '') === value)
    )
  })
}
async function data<T>(res: Response): Promise<T> {
  expect(res.status()).toBe(200)
  const envelope = await res.json()
  expect(envelope.code).toBe('OK')
  return envelope.data as T
}
function password() {
  const value = process.env.GLEAN_E2E_PASSWORD
  if (!value || value.length < 24) throw new Error('Generated CI-only browser password is required')
  return value
}
async function login(page: Page, username: string) {
  await page.goto('/')
  await page.getByLabel('用户名', { exact: true }).fill(username)
  await page.getByLabel('密码', { exact: true }).fill(password())
  const result = response(page, '/api/v1/catalog/papers')
  await page.getByRole('button', { name: '进入工作台', exact: true }).click()
  const papers = await data<PaperPage>(await result)
  await expect(page.getByTestId('paper-card').first()).toBeVisible()
  return papers
}
async function navigate(page: Page, name: string | RegExp) {
  const toggle = page.getByRole('button', { name: '打开导航', exact: true })
  if (await toggle.isVisible()) await toggle.click()
  const drawer = page.getByRole('dialog', { name: '导航', exact: true })
  const scope = (await drawer.isVisible()) ? drawer : page.locator('.desktop-sidebar')
  await scope.getByRole('button', { name, exact: typeof name === 'string' }).click()
  await expect(drawer).toBeHidden()
}
async function search(page: Page, q: string) {
  await page.getByRole('textbox', { name: '搜索论文或来源' }).fill(q)
  const result = response(page, '/api/v1/catalog/papers', 'GET', { q })
  await page.getByRole('button', { name: '搜索', exact: true }).click()
  return data<PaperPage>(await result)
}
const drawer = (page: Page) => page.getByRole('dialog', { name: '论文详情', exact: true })
const card = (page: Page, id: string) =>
  page.locator(`[data-testid="paper-card"][data-paper-id="${id}"]`)
async function openPaper(page: Page, id: string) {
  // Cached detail reads need not cause a redundant GET. Writes below explicitly
  // await the refreshed server detail before checking persisted source evidence.
  await card(page, id)
    .getByRole('button', { name: /^查看 .* 的详情$/ })
    .click()
  await expect(drawer(page)).toBeVisible()
  await drawer(page).getByText('论文管理', { exact: true }).click()
}
async function scope(page: Page, label: string, value: string) {
  const result = response(page, '/api/v1/catalog/papers', 'GET', { collectionScope: value })
  await page
    .getByRole('group', { name: '目录范围', exact: true })
    .getByRole('button', { name: label, exact: true })
    .click()
  return data<PaperPage>(await result)
}
function sourceSnapshot(paper: Paper) {
  return {
    evidence: paper.evidence,
    memberships: paper.memberships.map(({ topic, raw }) => ({ topic, raw })),
  }
}

test('真实管理流程：用户、窄屏主题、共享审核、纠错与审计历史', async ({ page, browser }, info) => {
  test.setTimeout(180_000)
  // One precise, one-shot failure, scoped to this page. Playwright removes the
  // route after its first match, so Retry must recover from the real Go API.
  let injectedFailures = 0
  await page.route(
    (url) => url.pathname === '/api/v1/catalog/access',
    async (route) => {
      expect(route.request().method()).toBe('GET')
      injectedFailures += 1
      await route.fulfill({
        status: 503,
        contentType: 'application/json',
        body: JSON.stringify({ code: 'UNAVAILABLE', message: 'Intentional acceptance fault' }),
      })
    },
    { times: 1 },
  )
  const initialFacetsResponse = response(page, '/api/v1/catalog/facets')
  const initial = await login(page, 'glean-browser-admin')
  const initialFacets = await data<Facets>(await initialFacetsResponse)
  const accessError = page.getByRole('status').filter({
    hasText: '管理权限暂时读取失败，阅读功能仍可使用。',
  })
  await expect(accessError).toBeVisible()
  await expect(page.getByRole('button', { name: '用户管理', exact: true })).toHaveCount(0)
  await expect(page.getByRole('button', { name: '研究主题', exact: true })).toHaveCount(0)
  await expect(page.getByTestId('paper-card').first()).toBeVisible()
  await page.screenshot({ path: info.outputPath('access-injected-503.png') })
  const recoveredAccess = response(page, '/api/v1/catalog/access')
  await accessError.getByRole('button', { name: '重试管理权限', exact: true }).click()
  expect(await data<{ canManage: boolean }>(await recoveredAccess)).toEqual({ canManage: true })
  expect(injectedFailures).toBe(1)
  await expect(accessError).toHaveCount(0)
  await expect(page.getByRole('button', { name: '重试管理权限', exact: true })).toHaveCount(0)
  await expect(
    page.locator('.desktop-sidebar').getByRole('button', { name: '用户管理', exact: true }),
  ).toBeVisible()
  await expect(
    page.locator('.desktop-sidebar').getByRole('button', { name: '研究主题', exact: true }),
  ).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)).toBe(
    true,
  )
  await page.screenshot({ path: info.outputPath('access-recovered-real-backend.png') })
  const selected = initial.items[0]
  expect(selected).toBeDefined()
  const userList = response(page, '/api/v1/users')
  await navigate(page, '用户管理')
  await data(await userList)
  await page.getByRole('button', { name: '编辑 glean-browser-admin', exact: true }).click()
  const userDialog = page.getByRole('dialog', { name: '编辑用户', exact: true })
  await expect(userDialog.getByLabel('用户类型', { exact: true })).toBeDisabled()
  await expect(userDialog.getByLabel('启用账号', { exact: true })).toBeDisabled()
  await userDialog.getByRole('button', { name: '取消', exact: true }).click()
  const username = 'glean-management-reader'
  await page.getByRole('button', { name: '新增用户', exact: true }).click()
  const create = page.getByRole('dialog', { name: '新增用户', exact: true })
  await create.getByLabel('用户名', { exact: true }).fill(username)
  await create.getByLabel('初始密码', { exact: true }).fill(password())
  await create.getByLabel('昵称', { exact: true }).fill('管理验收读者')
  await create.getByLabel('用户类型', { exact: true }).selectOption('user')
  const created = response(page, '/api/v1/users', 'POST')
  await create.getByRole('button', { name: '创建用户', exact: true }).click()
  const user = await data<{ id: string; isSuperAdmin: boolean }>(await created)
  expect(user.isSuperAdmin).toBe(false)
  await expect(create).toBeHidden()
  for (const enabled of [false, true]) {
    await page.getByRole('button', { name: `编辑 ${username}`, exact: true }).click()
    await userDialog.getByLabel('昵称', { exact: true }).fill('管理验收读者（已修改）')
    await userDialog.getByLabel('启用账号', { exact: true }).setChecked(enabled)
    const updated = response(page, `/api/v1/users/${user.id}`, 'PUT')
    await userDialog.getByRole('button', { name: '保存修改', exact: true }).click()
    const saved = await data<{ isEnable: boolean; nickname: string }>(await updated)
    expect(saved.isEnable).toBe(enabled)
    expect(saved.nickname).toBe('管理验收读者（已修改）')
    await expect(userDialog).toBeHidden()
    const row = page
      .getByRole('list', { name: '用户列表' })
      .getByRole('listitem')
      .filter({ hasText: username })
    await expect(row.getByText(enabled ? '已启用' : '已停用', { exact: true })).toBeVisible()
  }
  await page.screenshot({ path: info.outputPath('management-users.png') })
  const ordinaryContext = await browser.newContext({
    baseURL: info.project.use.baseURL,
    viewport: { width: 1440, height: 1080 },
  })
  try {
    const ordinary = await ordinaryContext.newPage()
    const ordinaryAccess = response(ordinary, '/api/v1/catalog/access')
    const papers = await login(ordinary, username)
    expect(await data<{ canManage: boolean }>(await ordinaryAccess)).toEqual({ canManage: false })
    // Refresh rechecks capabilities against Go; it must not grant reader access.
    const refreshedAccess = response(ordinary, '/api/v1/catalog/access')
    await ordinary.getByRole('button', { name: '刷新当前视图', exact: true }).click()
    expect(await data<{ canManage: boolean }>(await refreshedAccess)).toEqual({ canManage: false })
    await expect(ordinary.getByRole('button', { name: '用户管理', exact: true })).toHaveCount(0)
    await expect(ordinary.getByRole('button', { name: '研究主题', exact: true })).toHaveCount(0)
    await card(ordinary, papers.items[0].id)
      .getByRole('button', { name: /^查看 .* 的详情$/ })
      .click()
    await expect(drawer(ordinary)).toBeVisible()
    await expect(drawer(ordinary).getByText('论文管理', { exact: true })).toHaveCount(0)
  } finally {
    await ordinaryContext.close()
  }

  await page.setViewportSize({ width: 393, height: 852 })
  await navigate(page, '研究主题')
  await page.getByRole('button', { name: '新增研究主题', exact: true }).click()
  const topicDialog = page.getByRole('dialog', { name: '新增研究主题', exact: true })
  const topicName = '管理验收：胶质瘤世界模型'
  await topicDialog.getByLabel('主题名称', { exact: true }).fill(topicName)
  await topicDialog
    .getByLabel('主题说明', { exact: true })
    .fill('真实接口创建的空主题，不添加演示论文。')
  await topicDialog.getByLabel('纳入规则', { exact: true }).fill('关注胶质瘤相关世界模型研究。')
  await topicDialog
    .getByLabel('检索词组', { exact: true })
    .fill('glioma | glioblastoma\nworld model')
  await topicDialog.getByLabel('参与后续扫描', { exact: true }).check()
  const topicCreated = response(page, '/api/v1/catalog/topics', 'POST')
  await topicDialog.getByRole('button', { name: '保存主题', exact: true }).click()
  const topic = await data<Topic>(await topicCreated)
  expect(topic.slug).toBeTruthy()
  expect(topic.version).toBe(1)
  expect(topic.termGroups?.map((group) => [...group].sort())).toEqual([
    ['glioblastoma', 'glioma'],
    ['world model'],
  ])
  await expect(topicDialog).toBeHidden()
  await page.getByRole('button', { name: `编辑主题 ${topicName}`, exact: true }).click()
  const editTopic = page.getByRole('dialog', { name: '编辑研究主题', exact: true })
  await editTopic
    .getByLabel('主题说明', { exact: true })
    .fill('已修改说明；暂停扫描，保留检索规则。')
  await editTopic.getByLabel('参与后续扫描', { exact: true }).uncheck()
  const topicUpdated = response(page, `/api/v1/catalog/topics/${topic.slug}`, 'PUT')
  await editTopic.getByRole('button', { name: '保存主题', exact: true }).click()
  const updatedTopic = await data<Topic>(await topicUpdated)
  expect(updatedTopic.version).toBe(2)
  expect(updatedTopic.searchEnabled).toBe(false)
  await expect(editTopic).toBeHidden()
  await page.screenshot({ path: info.outputPath('management-topics-mobile.png') })
  const emptyResult = response(page, '/api/v1/catalog/papers', 'GET', { topic: topic.slug })
  await navigate(page, new RegExp(`^${topicName}`))
  const empty = await data<PaperPage>(await emptyResult)
  expect(empty.total).toBe(0)
  expect(empty.items).toEqual([])
  await expect(page.getByTestId('paper-card')).toHaveCount(0)
  await expect(page.getByRole('heading', { name: '这个主题还没有论文', exact: true })).toBeVisible()
  await expect(page.getByText(/保存主题不会启动扫描/)).toBeVisible()
  await expect(page.getByText(/此主题已暂停后续检索/)).toBeVisible()
  await page.screenshot({ path: info.outputPath('management-empty-topic-mobile.png') })
  // Source progress remains a read-only destination; opening it never starts a scan.
  await page.getByRole('button', { name: '查看采集进度', exact: true }).click()
  await expect(page.getByRole('heading', { name: '来源健康', exact: true })).toBeVisible()
  await expect(page.getByText('定时计划未启用', { exact: true })).toBeVisible()
  await navigate(page, new RegExp(`^${topicName}`))
  await expect(page.getByRole('heading', { name: '这个主题还没有论文', exact: true })).toBeVisible()

  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)).toBe(
    true,
  )

  await page.setViewportSize({ width: 1440, height: 1080 })
  await navigate(page, /^生物医学 × AI/)
  await expect(card(page, selected.id)).toBeVisible()
  const detailResult = response(page, `/api/v1/catalog/papers/${selected.id}`)
  await openPaper(page, selected.id)
  const original = await data<Paper>(await detailResult)
  expect(original.isMock).toBe(false)
  expect(original.origin).toBe('historical')
  const originalSource = sourceSnapshot(original)
  const review = drawer(page).getByRole('form', { name: '审核 生物医学 × AI', exact: true })
  const reviewPath = `/api/v1/catalog/papers/${selected.id}/topics/biomed-ai/review`
  for (const status of ['excluded', 'included'] as const) {
    await review.getByLabel('审核结论', { exact: true }).selectOption(status)
    await review.getByLabel('审核备注', { exact: true }).fill(`真实浏览器验收：${status}`)
    await review.getByLabel('手动标签', { exact: true }).fill('人工核验，浏览器验收')
    const put = response(page, reviewPath, 'PUT')
    const refreshed = response(page, `/api/v1/catalog/papers/${selected.id}`)
    await review.getByRole('button', { name: '保存审核', exact: true }).click()
    const saved = await data<ManualReview>(await put)
    expect(saved.status).toBe(status)
    expect(saved.version).toBe(status === 'excluded' ? 1 : 2)
    expect(saved.labels).toEqual(['人工核验', '浏览器验收'])
    expect(sourceSnapshot(await data<Paper>(await refreshed))).toEqual(originalSource)
    await expect(review.getByRole('button', { name: '保存审核', exact: true })).toBeEnabled()
    if (status === 'excluded') {
      await review.scrollIntoViewIfNeeded()
      await page.screenshot({ path: info.outputPath('management-review.png') })
      await drawer(page).getByRole('button', { name: '关闭', exact: true }).click()
      await expect(card(page, selected.id)).toHaveCount(0)
      await scope(page, '全部记录', 'all')
      const found = await search(page, original.doi || original.title)
      expect(found.items.map((paper) => paper.id)).toContain(selected.id)
      await openPaper(page, selected.id)
    }
  }
  await drawer(page).getByRole('button', { name: '关闭', exact: true }).click()
  const restoredList = await scope(page, '精选目录', 'curated')
  expect(restoredList.items.map((paper) => paper.id)).toContain(selected.id)
  await openPaper(page, selected.id)
  const correction = drawer(page).getByRole('form', { name: '元数据纠错', exact: true })
  const correctedTitle = 'Glean management browser verified metadata correction'
  const correctedYear = original.year === 2025 ? 2024 : 2025
  await correction.getByLabel('纠正原文标题', { exact: true }).check()
  await correction.getByLabel('原文标题', { exact: true }).fill(correctedTitle)
  await correction.getByLabel('纠正发表年份', { exact: true }).check()
  await correction.getByLabel('发表年份', { exact: true }).fill(String(correctedYear))
  await correction
    .getByLabel('纠错原因', { exact: true })
    .fill('浏览器验收：验证可撤销元数据覆盖，不改来源记录。')
  const correctionPath = `/api/v1/catalog/papers/${selected.id}/correction`
  async function saveCorrection(expectedVersion: number) {
    const put = response(page, correctionPath, 'PUT')
    const refreshed = response(page, `/api/v1/catalog/papers/${selected.id}`)
    await correction.getByRole('button', { name: '保存纠错', exact: true }).click()
    const saved = await data<Correction>(await put)
    expect(saved.version).toBe(expectedVersion)
    const effective = await data<Paper>(await refreshed)
    expect(sourceSnapshot(effective)).toEqual(originalSource)
    await expect(correction.getByRole('button', { name: '保存纠错', exact: true })).toBeEnabled()
    return effective
  }
  const correctedFacets = response(page, '/api/v1/catalog/facets')
  const corrected = await saveCorrection(1)
  const effectiveFacets = await data<Facets>(await correctedFacets)
  const yearCount = (facets: Facets, year: number) =>
    facets.years.find((facet) => facet.value === String(year))?.count ?? 0
  expect(yearCount(effectiveFacets, correctedYear)).toBe(
    yearCount(initialFacets, correctedYear) + 1,
  )
  expect(yearCount(effectiveFacets, original.year)).toBe(
    yearCount(initialFacets, original.year) - 1,
  )
  expect(corrected.title).toBe(correctedTitle)
  expect(corrected.year).toBe(correctedYear)
  await correction.scrollIntoViewIfNeeded()
  await page.screenshot({ path: info.outputPath('management-correction.png') })
  await drawer(page).getByRole('button', { name: '关闭', exact: true }).click()
  const correctedSearch = await search(page, correctedTitle)
  expect(correctedSearch.items.map((paper) => paper.id)).toContain(selected.id)
  await openPaper(page, selected.id)
  await correction.getByLabel('纠正原文标题', { exact: true }).uncheck()
  await correction.getByLabel('纠正发表年份', { exact: true }).uncheck()
  await correction.getByLabel('纠错原因', { exact: true }).fill('验收结束，恢复全部来源字段。')
  const restored = await saveCorrection(2)
  expect(restored.title).toBe(original.title)
  expect(restored.year).toBe(original.year)
  expect(restored.correction?.fields).toEqual({})
  const historyResult = response(page, `/api/v1/catalog/papers/${selected.id}/history`)
  await drawer(page).getByText('修改历史', { exact: true }).click()
  const history = await data<{ items: CatalogHistory[] }>(await historyResult)
  expect(history.items).toHaveLength(4)
  expect(history.items.every((item) => item.actorId === '9')).toBe(true)
  expect(JSON.stringify(history.items)).toContain(correctedTitle)
  await expect(drawer(page).getByText('查看修改前后', { exact: true })).toHaveCount(4)
  await drawer(page).getByText('查看修改前后', { exact: true }).first().click()
  await expect(drawer(page).locator('pre').filter({ hasText: '修改前' }).first()).toBeVisible()
  await drawer(page).getByRole('button', { name: '关闭', exact: true }).click()
  await search(page, '')
  await expect(card(page, selected.id)).toBeVisible()
})

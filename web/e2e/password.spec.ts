import { expect, test, type Page } from '@playwright/test'

// Runs once after management, against its CI-only admin. No token injection,
// route mocking, traces or persisted auth state; only UI-generated real requests.
async function login(page: Page, password: string) {
  await page.goto('/')
  await page.getByLabel('用户名', { exact: true }).fill('glean-browser-admin')
  await page.getByLabel('密码', { exact: true }).fill(password)
  await page.getByRole('button', { name: '进入工作台', exact: true }).click()
  await expect(page.getByRole('heading', { name: '论文工作台', exact: true })).toBeVisible()
}
async function openPassword(page: Page) {
  await page.getByRole('button', { name: /个人研究空间/ }).click()
  await page.getByRole('button', { name: '修改密码', exact: true }).click()
  await expect(page.getByRole('dialog', { name: '修改密码', exact: true })).toBeVisible()
}
async function fill(page: Page, current: string, next: string, confirm = next) {
  await page.getByLabel('当前密码', { exact: true }).fill(current)
  await page.getByLabel('新密码', { exact: true }).fill(next)
  await page.getByLabel('确认密码', { exact: true }).fill(confirm)
}
test('真实改密：校验、取消、旧会话撤销与新密码登录', async ({ page, browser, baseURL }) => {
  const original = process.env.GLEAN_E2E_PASSWORD
  if (!original || original.length < 24)
    throw new Error('Generated CI-only browser password is required')
  const next = process.env.GLEAN_E2E_NEW_PASSWORD
  if (!next || next.length < 24 || next === original)
    throw new Error('Distinct generated CI-only replacement password is required')
  const otherContext = await browser.newContext({ baseURL })
  const other = await otherContext.newPage()
  try {
    await login(page, original)
    await login(other, original)
    await openPassword(page)
    await fill(page, original, next, `${next}-mismatch`)
    await page.getByRole('button', { name: '确认修改' }).click()
    await expect(page.getByRole('alert')).toHaveText(/不一致/)
    await page.getByRole('button', { name: '取消', exact: true }).click()
    await expect(page.getByRole('dialog', { name: '你的研究空间' })).toBeVisible()
    await page.getByRole('button', { name: '修改密码', exact: true }).click()
    await expect(page.getByLabel('当前密码', { exact: true })).toBeEmpty()
    await fill(page, `${original}-wrong`, next)
    const rejected = page.waitForResponse(
      (res) => new URL(res.url()).pathname === '/api/v1/auth/password',
    )
    await page.getByRole('button', { name: '确认修改' }).click()
    expect((await rejected).status()).toBe(400)
    await expect(page.getByRole('alert')).toHaveText(/请检查当前密码/)
    await fill(page, original, next)
    const changed = page.waitForResponse(
      (res) => new URL(res.url()).pathname === '/api/v1/auth/password',
    )
    await page.getByRole('button', { name: '确认修改' }).click()
    expect((await changed).status()).toBe(200)
    await expect(page.getByRole('status')).toHaveText(/密码已修改/)
    await expect(page.getByRole('button', { name: '进入工作台' })).toBeVisible()
    // A fresh authenticated query in the other session must be rejected, not served from cache.
    await other.getByRole('textbox', { name: '搜索论文或来源' }).fill('session-revocation-check')
    await other.getByRole('button', { name: '搜索', exact: true }).click()
    await expect(other.getByRole('button', { name: '进入工作台' })).toBeVisible()
    await expect(other.getByRole('status')).toHaveText(/会话已结束/)
    await page.getByLabel('用户名', { exact: true }).fill('glean-browser-admin')
    await page.getByLabel('密码', { exact: true }).fill(original)
    await page.getByRole('button', { name: '进入工作台' }).click()
    await expect(page.getByRole('alert')).toHaveText(/用户名或密码不正确/)
    await login(page, next)
    expect(await page.evaluate(() => localStorage.length + sessionStorage.length)).toBe(0)
  } finally {
    await otherContext.close()
  }
})

import { readFileSync } from 'node:fs'
import { expect, test } from '@playwright/test'
import type { ProtocolPreview, Topic } from '../src/lib/types'

test('非生物医学协议经真实预览、确认、保存和重载后保留完整计划', async ({ page }, info) => {
  const password = process.env.GLEAN_E2E_PASSWORD
  if (!password) throw new Error('Disposable browser password required')
  await page.goto('/')
  await page.getByLabel('用户名', { exact: true }).fill('glean-browser-admin')
  await page.getByLabel('密码', { exact: true }).fill(password)
  await page.getByRole('button', { name: '进入工作台', exact: true }).click()
  await page
    .locator('.desktop-sidebar')
    .getByRole('button', { name: '研究主题', exact: true })
    .click()
  await page.getByRole('button', { name: '新增研究主题', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: '新增研究主题', exact: true })
  const name = `协议验收制氢-${Date.now()}`
  await dialog.getByLabel('主题名称', { exact: true }).fill(name)
  await dialog.getByText('导入完整检索协议', { exact: true }).click()
  const raw = readFileSync(
    new URL('../../internal/retrieval/testdata/hydrogen-plan.json', import.meta.url),
    'utf8',
  )
  await dialog.getByLabel('检索协议 JSON', { exact: true }).fill(raw)
  const save = dialog.getByRole('button', { name: '保存主题', exact: true })
  await expect(save).toBeDisabled()
  const previewResponse = page.waitForResponse(
    (r) =>
      new URL(r.url()).pathname === '/api/v1/catalog/topics/protocol-preview' &&
      r.request().method() === 'POST',
  )
  await dialog.getByRole('button', { name: '预览协议与语义差异' }).click()
  const response = await previewResponse
  expect(response.status()).toBe(200)
  const preview = (await response.json()).data as ProtocolPreview
  expect(preview.protocol.mode).toBe('plan')
  expect(preview.executable).toBe(false)
  expect(preview.externalSources).toContain('openalex')
  expect(Array.isArray(preview.protocol.external_queries)).toBe(true)
  expect((preview.protocol.external_queries as unknown[]).length).toBeGreaterThan(0)
  await expect(dialog.getByText('仅保存计划', { exact: false })).toBeVisible()
  await expect(dialog.getByLabel('参与后续扫描', { exact: true })).toBeDisabled()
  await expect(dialog.getByLabel('参与后续扫描', { exact: true })).not.toBeChecked()
  await expect(save).toBeDisabled()
  await dialog.getByLabel('已核对来源、日期和语义差异', { exact: true }).check()
  await page.screenshot({ path: info.outputPath('protocol-preview.png') })
  const createdResponse = page.waitForResponse(
    (r) =>
      new URL(r.url()).pathname === '/api/v1/catalog/topics' && r.request().method() === 'POST',
  )
  await save.click()
  const created = await createdResponse
  expect(created.status()).toBe(200)
  const topic = (await created.json()).data as Topic
  expect(topic.searchEnabled).toBe(false)
  expect(topic.searchProtocol).toEqual(preview.protocol)
  await expect(dialog).toBeHidden()
  await page.reload()
  // 会话凭据只在内存中；整页重载后按真实登录流程重新认证。
  await page.getByLabel('用户名', { exact: true }).fill('glean-browser-admin')
  await page.getByLabel('密码', { exact: true }).fill(password)
  await page.getByRole('button', { name: '进入工作台', exact: true }).click()
  await page
    .locator('.desktop-sidebar')
    .getByRole('button', { name: '研究主题', exact: true })
    .click()
  await page.getByRole('button', { name: `编辑主题 ${name}`, exact: true }).click()
  const edit = page.getByRole('dialog', { name: '编辑研究主题', exact: true })
  const saved = JSON.parse(await edit.getByLabel('检索协议 JSON', { exact: true }).inputValue())
  expect(saved).toEqual(preview.protocol)
  // 任何新编辑都撤销旧确认，不能拿之前的预览确认提交新内容。
  await edit.getByLabel('检索协议 JSON', { exact: true }).fill('{"invalid":true}')
  await expect(edit.getByRole('button', { name: '保存主题', exact: true })).toBeDisabled()
  await edit.getByRole('button', { name: '预览协议与语义差异' }).click()
  await expect(edit.getByRole('alert')).toBeVisible()
  await expect(edit.getByRole('button', { name: '保存主题', exact: true })).toBeDisabled()
  await page.screenshot({ path: info.outputPath('protocol-invalid-blocked.png') })
})

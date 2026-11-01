import { execFileSync } from 'node:child_process'
import { mkdtempSync, mkdirSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { gzipSync } from 'node:zlib'
import { expect, it } from 'vitest'

it('counts HTML preloads and recursive static imports once, excluding dynamic imports', () => {
  const root = mkdtempSync(join(tmpdir(), 'glean-bundle-size-'))
  try {
    mkdirSync(join(root, 'assets'))
    writeFileSync(
      join(root, 'index.html'),
      '<script type="module" src="/assets/index.js"></script><link rel="modulepreload" href="/assets/shared.js">',
    )
    const files: Record<string, string> = {
      'index.js': 'import "./shared.js"; import("./later.js");',
      'shared.js': 'export { value } from "./nested.js";',
      'nested.js': 'export const value = 1;',
      'later.js': 'export const deferred = true;',
      'style.css': 'body { color: green; }',
    }
    for (const [file, source] of Object.entries(files))
      writeFileSync(join(root, 'assets', file), source)
    const result = JSON.parse(
      execFileSync(process.execPath, ['scripts/bundle-size.mjs', root], { encoding: 'utf8' }),
    )
    expect(result.initial.assets.map((asset: { file: string }) => asset.file)).toEqual([
      'assets/index.js',
      'assets/nested.js',
      'assets/shared.js',
    ])
    const initial = ['index.js', 'nested.js', 'shared.js'].map((name) => files[name])
    expect(result.initial.bytes).toBe(
      initial.reduce((sum, source) => sum + Buffer.byteLength(source), 0),
    )
    expect(result.initial.gzipBytes).toBe(
      initial.reduce((sum, source) => sum + gzipSync(source).length, 0),
    )
    expect(result.deferred.assets.map((asset: { file: string }) => asset.file)).toEqual([
      'assets/later.js',
    ])
    expect(result.css.bytes).toBe(Buffer.byteLength(files['style.css']))
  } finally {
    rmSync(root, { recursive: true, force: true })
  }
})

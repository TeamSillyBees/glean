import { readFileSync, readdirSync } from 'node:fs'
import { dirname, resolve, relative } from 'node:path'
import { gzipSync } from 'node:zlib'
import ts from 'typescript'

// Count the initial static dependency closure, not just the entry filename.
// Dynamic imports are deliberately excluded until their feature is opened.
const root = resolve(process.argv[2] || 'dist')
const html = readFileSync(resolve(root, 'index.html'), 'utf8')
const initial = new Set()
function visit(file) {
  if (initial.has(file)) return
  initial.add(file)
  const source = ts.createSourceFile(file, readFileSync(file, 'utf8'), ts.ScriptTarget.Latest)
  for (const node of source.statements) {
    if (!(ts.isImportDeclaration(node) || ts.isExportDeclaration(node))) continue
    const specifier = node.moduleSpecifier
    if (!specifier || !ts.isStringLiteral(specifier)) continue
    const path = specifier.text
    if (!path.startsWith('.') && !path.startsWith('/'))
      throw new Error(`Unexpected external static import: ${path}`)
    visit(path.startsWith('/') ? resolve(root, path.slice(1)) : resolve(dirname(file), path))
  }
}
for (const tag of html.matchAll(/<(?:script|link)\b[^>]*>/g)) {
  if (tag[0].startsWith('<link') && !/rel=["']modulepreload["']/.test(tag[0])) continue
  const path = tag[0].match(/(?:src|href)=["']([^"']+\.js)["']/)?.[1]
  if (path) visit(resolve(root, path.replace(/^\//, '')))
}
if (!initial.size) throw new Error('No initial JavaScript found')
function sizes(files) {
  const assets = files.map((file) => {
    const bytes = readFileSync(file)
    return { file: relative(root, file), bytes: bytes.length, gzipBytes: gzipSync(bytes).length }
  })
  return {
    assets,
    bytes: assets.reduce((sum, asset) => sum + asset.bytes, 0),
    gzipBytes: assets.reduce((sum, asset) => sum + asset.gzipBytes, 0),
  }
}
const assets = readdirSync(resolve(root, 'assets')).map((file) => resolve(root, 'assets', file))
console.log(
  JSON.stringify(
    {
      compression: 'node:zlib gzipSync defaults, each asset compressed separately',
      initial: sizes([...initial].sort()),
      deferred: sizes(assets.filter((file) => file.endsWith('.js') && !initial.has(file)).sort()),
      css: sizes(assets.filter((file) => file.endsWith('.css')).sort()),
    },
    null,
    2,
  ),
)

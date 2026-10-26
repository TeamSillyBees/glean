/// <reference types="node" />
// @vitest-environment node
import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'
const read = (name: string) => readFileSync(new URL(`../../${name}`, import.meta.url), 'utf8')
const dockerfile = read('Dockerfile')
const nginx = read('nginx.conf')
const ignored = read('.dockerignore').split('\n')

describe('static container deployment contract (not a Docker runtime test)', () => {
  it('builds with official Node 24 and serves only dist in official nginx as non-root', () => {
    expect(dockerfile).toMatch(/^FROM node:24-bookworm-slim AS build$/m)
    expect(dockerfile).toMatch(/^RUN npm ci --no-audit --no-fund$/m)
    expect(dockerfile).toMatch(/^FROM nginx:stable-alpine AS runtime$/m)
    expect(dockerfile).toContain('COPY --from=build /app/dist/ /usr/share/nginx/html/')
    expect(dockerfile).toContain('USER nginx\nRUN nginx -t')
    expect(dockerfile).not.toMatch(/^COPY \.\s/m)
    expect(dockerfile).not.toMatch(/^ARG .*?(KEY|TOKEN|PASSWORD|SECRET)/m)
  })
  it('excludes local secrets and existing artifacts from the build context', () => {
    for (const entry of ['.env', '.env.*', '.npmrc', '*.pem', '*.key', 'node_modules', 'dist']) {
      expect(ignored).toContain(entry)
    }
  })
  it('proxies the exact API path and bearer header without replacing failures with the SPA', () => {
    expect(nginx).toContain('set $glean_backend backend:8080;')
    expect(nginx).toContain('resolver 127.0.0.11 valid=30s ipv6=off;')
    expect(nginx).toContain('location ^~ /api/')
    expect(nginx).toContain('proxy_pass http://$glean_backend;')
    expect(nginx).not.toContain('proxy_pass http://$glean_backend/;')
    expect(nginx).toContain('proxy_set_header Authorization $http_authorization;')
    expect(nginx).toContain('proxy_intercept_errors off;')
    expect(nginx).toContain('proxy_cache off;')
  })
  it('supports SPA refreshes while missing assets and API root remain real 404s', () => {
    expect(nginx).toMatch(/location \/ \{\s*try_files \$uri \$uri\/ \/index\.html;/)
    expect(nginx).toMatch(/location \^~ \/assets\/ \{\s*try_files \$uri =404;/)
    expect(nginx).toMatch(/location = \/api \{\s*return 404;/)
  })
  it('checks static entry-file health without contacting the backend', () => {
    expect(nginx).toMatch(
      /location = \/healthz \{\s*access_log off;\s*try_files \/index\.html =503;/,
    )
    expect(dockerfile).toContain('http://127.0.0.1:8080/healthz')
    expect(dockerfile).toContain(
      'HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3',
    )
  })
  it('keeps runtime writes under tmp and limits CSP connections to the same origin', () => {
    expect(nginx).toContain('pid /tmp/nginx.pid;')
    expect(nginx).toContain('client_body_temp_path /tmp/client_temp;')
    expect(nginx).toContain("connect-src 'self'")
    expect(nginx).not.toContain('$request_body')
    expect(nginx).not.toMatch(
      /log_format[^;]*\$(?:request|args|http_authorization|http_cookie)(?:\s|[";])/s,
    )
  })
})

const compose = readFileSync(new URL('../../../docker-compose.yml', import.meta.url), 'utf8')
function serviceBlock(name: string) {
  const lines = compose.split('\n')
  const start = lines.findIndex((line) => line === `  ${name}:`)
  if (start < 0) throw new Error(`Compose service missing: ${name}`)
  const next = lines.findIndex((line, index) => index > start && /^  [\w-]+:/.test(line))
  return lines.slice(start, next < 0 ? undefined : next).join('\n')
}

describe('repository Compose / frontend alignment', () => {
  it('builds the web context, publishes its actual port locally and names the proxy backend', () => {
    const web = serviceBlock('web')
    const backend = serviceBlock('backend')
    expect(web).toMatch(/build:\s*\.\/web/)
    expect(web).toMatch(/127\.0\.0\.1:\d+:8080/)
    expect(web).toMatch(/depends_on:[\s\S]*(?:-\s*backend|\s+backend:)/)
    expect(backend).toMatch(/INITRA_SERVER_ADDR:\s*["']?(?:0\.0\.0\.0)?:8080/)
    expect(nginx).toContain('set $glean_backend backend:8080;')
    expect(nginx).toContain('listen 8080;')
  })
})

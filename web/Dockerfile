# Build context: web/ (docker build -t glean-web:local ./web from the repo root).
FROM node:24-bookworm-slim AS build

WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci --no-audit --no-fund

# Copy an explicit build allowlist; local .env files and credentials never enter a layer.
COPY index.html tsconfig.json tsconfig.app.json tsconfig.node.json vite.config.ts ./
COPY src ./src
COPY public ./public
RUN npm run build

FROM nginx:stable-alpine AS runtime

COPY nginx.conf /etc/nginx/nginx.conf
COPY --from=build /app/dist/ /usr/share/nginx/html/

# The custom configuration uses an unprivileged port and /tmp for writable paths.
USER nginx
RUN nginx -t
EXPOSE 8080
STOPSIGNAL SIGQUIT
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD wget -q -T 3 -O /dev/null http://127.0.0.1:8080/healthz || exit 1

# No runtime envsubst or entrypoint writes; only prebuilt static files are served.
ENTRYPOINT ["nginx"]
CMD ["-g", "daemon off;"]

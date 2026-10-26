FROM golang:1.26.3-bookworm AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server \
    && mkdir -p /out/var/logs /out/var/uploads

FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app

COPY --from=builder --chown=65532:65532 /out/server /app/server
COPY --from=builder --chown=65532:65532 /out/var /app/var
COPY --from=builder --chown=65532:65532 /src/configs/rbac_model.conf /app/configs/rbac_model.conf

EXPOSE 8080

USER 65532:65532
ENTRYPOINT ["/app/server"]

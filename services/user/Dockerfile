FROM golang:1.27.4-alpine AS build

WORKDIR /src

RUN apk add --no-cache ca-certificates

COPY services/user/go.mod services/user/go.sum ./
RUN go mod download

COPY services/user/ ./

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api \
    && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/migrate ./cmd/migrate \
    && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/admin ./cmd/admin \
    && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/keygen ./cmd/keygen

FROM alpine:3.21

RUN apk add --no-cache ca-certificates \
    && adduser -D -H -u 10001 app

WORKDIR /app

COPY --from=build /out/ /app/

USER 10001

# 3001 is the API; 9091 is the metrics listener (METRICS_ADDR).
EXPOSE 3001 9091

# Override the URL when PORT or APP_ENV changes the listen address.
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
    CMD wget -qO- http://127.0.0.1:3001/healthz > /dev/null || exit 1

ENTRYPOINT ["/app/api"]

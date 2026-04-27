FROM golang:1.26-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./

RUN go mod download

COPY . .

ARG VERSION=dev
ARG COMMIT=none
ARG BUILD_DATE=unknown
ARG TARGETARCH
ARG TARGETOS=linux

RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -ldflags="-s -w \
      -X 'main.Version=${VERSION}' \
      -X 'main.Commit=${COMMIT}' \
      -X 'main.BuildDate=${BUILD_DATE}'" \
    -o ./CLIProxyAPI ./cmd/server/

FROM alpine:3.22.0

RUN apk add --no-cache tzdata ca-certificates

RUN mkdir -p /CLIProxyAPI/panel

COPY --from=builder /app/CLIProxyAPI /CLIProxyAPI/CLIProxyAPI

COPY config.example.yaml /CLIProxyAPI/config.example.yaml
COPY config.template.yaml /CLIProxyAPI/config.template.yaml
COPY docker-entrypoint.sh /CLIProxyAPI/docker-entrypoint.sh

# Management panel — built by CI from Cli-Proxy-API-Management-Center and
# placed at panel/management.html before docker build context is sent.
# If absent the server auto-downloads from GitHub on first start.
COPY panel/ /CLIProxyAPI/panel/

WORKDIR /CLIProxyAPI

RUN chmod +x docker-entrypoint.sh

EXPOSE 8317

ENV TZ=Asia/Shanghai
ENV MANAGEMENT_STATIC_PATH=/CLIProxyAPI/panel/management.html

RUN cp /usr/share/zoneinfo/${TZ} /etc/localtime && echo "${TZ}" > /etc/timezone

ENTRYPOINT ["./docker-entrypoint.sh"]
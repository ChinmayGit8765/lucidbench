# syntax=docker/dockerfile:1

FROM node:22-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w -X github.com/ChinmayGit8765/lucidbench/internal/version.Version=${VERSION}" \
    -o /out/lucidd ./cmd/lucidd \
 && CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w -X github.com/ChinmayGit8765/lucidbench/internal/version.Version=${VERSION}" \
    -o /out/lucid ./cmd/lucid

# The kind library drives the cluster through the docker CLI, so the runtime
# image carries docker-cli (talking to the mounted docker socket).
FROM alpine:3.21
RUN apk add --no-cache docker-cli ca-certificates
COPY --from=build /out/lucidd /out/lucid /usr/local/bin/
COPY LICENSE THIRD_PARTY_NOTICES.md /usr/share/doc/lucidbench/
COPY web/public/licenses/ /usr/share/doc/lucidbench/licenses/
EXPOSE 7420
ENTRYPOINT ["/usr/local/bin/lucidd"]

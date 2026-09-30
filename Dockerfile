# syntax=docker/dockerfile:1

FROM node:22-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.25 AS build
WORKDIR /src
COPY go.mod ./
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

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/lucidd /out/lucid /usr/local/bin/
EXPOSE 7420
ENTRYPOINT ["/usr/local/bin/lucidd"]

# syntax=docker/dockerfile:1.7

FROM node:22-alpine AS ui-deps
WORKDIR /src/ui
COPY ui/package*.json ./
RUN --mount=type=cache,target=/root/.npm npm ci --prefer-offline --no-audit

FROM ui-deps AS ui-build
COPY ui/ ./
RUN npm run build

FROM golang:1.26-alpine AS go-base
WORKDIR /src
RUN apk add --no-cache ca-certificates
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY ui/embed.go ./ui/embed.go
COPY --from=ui-build /src/ui/dist ./ui/dist

FROM go-base AS verify
RUN cp go.mod /tmp/go.mod \
  && cp go.sum /tmp/go.sum \
  && go mod tidy \
  && cmp -s go.mod /tmp/go.mod \
  && cmp -s go.sum /tmp/go.sum
RUN --mount=type=cache,target=/go/pkg/mod \
  --mount=type=cache,target=/root/.cache/go-build \
  go test ./...

FROM verify AS build
ARG TARGETOS=linux
ARG TARGETARCH=amd64
ENV CGO_ENABLED=0
RUN --mount=type=cache,target=/go/pkg/mod \
  --mount=type=cache,target=/root/.cache/go-build \
  GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/northscope ./cmd/northscope

FROM gcr.io/distroless/static-debian12:nonroot AS runtime
WORKDIR /
COPY --from=build /out/northscope /northscope
EXPOSE 8080
USER 65532:65532
ENTRYPOINT ["/northscope"]

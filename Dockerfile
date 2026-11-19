# syntax=docker/dockerfile:1.7

FROM --platform=$BUILDPLATFORM node:26-alpine@sha256:c6ad9e9c1d80f6eb607c26b4b0a132ff7a2344fad1c78ea3baa2a7d4c8de6d94 AS ui-deps
WORKDIR /src/ui
COPY ui/package*.json ./
RUN --mount=type=cache,target=/root/.npm npm ci --prefer-offline --no-audit

FROM ui-deps AS ui-build
COPY ui/ ./
RUN npm run test:ui-smoke && npm run build

FROM --platform=$BUILDPLATFORM golang:1.27.3-alpine@sha256:d200d2dd76b03a99b52651fd1325aff7dcd35422c35c01bac56571b0507f3deb AS go-base
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
  && tr -d '\r' </tmp/go.mod >/tmp/go.mod.normalized \
  && tr -d '\r' <go.mod >/tmp/go.mod.after \
  && tr -d '\r' </tmp/go.sum >/tmp/go.sum.normalized \
  && tr -d '\r' <go.sum >/tmp/go.sum.after \
  && cmp -s /tmp/go.mod.normalized /tmp/go.mod.after \
  && cmp -s /tmp/go.sum.normalized /tmp/go.sum.after
RUN --mount=type=cache,target=/go/pkg/mod \
  --mount=type=cache,target=/root/.cache/go-build \
  go test ./...

FROM verify AS build
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
ENV CGO_ENABLED=0
RUN --mount=type=cache,target=/go/pkg/mod \
  --mount=type=cache,target=/root/.cache/go-build \
  GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath \
  -ldflags="-s -w -X github.com/emircanagac/northscope/internal/buildinfo.Version=${VERSION}" \
  -o /out/northscope ./cmd/northscope

FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab AS runtime
WORKDIR /
COPY --from=build /out/northscope /northscope
EXPOSE 8080
USER 65532:65532
ENTRYPOINT ["/northscope"]

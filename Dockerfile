# syntax=docker/dockerfile:1.7

FROM node:26-alpine@sha256:e88a35be04478413b7c71c455cd9865de9b9360e1f43456be5951032d7ac1a66 AS ui-deps
WORKDIR /src/ui
COPY ui/package*.json ./
RUN --mount=type=cache,target=/root/.npm npm ci --prefer-offline --no-audit

FROM ui-deps AS ui-build
COPY ui/ ./
RUN npm run test:ui-smoke && npm run build

FROM golang:1.26.5-alpine@sha256:0178a641fbb4858c5f1b48e34bdaabe0350a330a1b1149aabd498d0699ff5fb2 AS go-base
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
ARG TARGETOS=linux
ARG TARGETARCH=amd64
ENV CGO_ENABLED=0
RUN --mount=type=cache,target=/go/pkg/mod \
  --mount=type=cache,target=/root/.cache/go-build \
  GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/northscope ./cmd/northscope

FROM gcr.io/distroless/static-debian12:nonroot@sha256:f5b485ea962d9bd1186b2f6b3a061191539b905b82ec395de78cbfae51f20e35 AS runtime
WORKDIR /
COPY --from=build /out/northscope /northscope
EXPOSE 8080
USER 65532:65532
ENTRYPOINT ["/northscope"]

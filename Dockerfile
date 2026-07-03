FROM node:22-alpine AS ui-build
WORKDIR /src/ui
COPY ui/package*.json ./
RUN npm ci
COPY ui/ ./
RUN npm run build

FROM golang:1.22-alpine AS go-build
WORKDIR /src
RUN apk add --no-cache ca-certificates
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY ui/embed.go ./ui/embed.go
COPY --from=ui-build /src/ui/dist ./ui/dist
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/northscope ./cmd/northscope

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /
COPY --from=go-build /out/northscope /northscope
EXPOSE 8080
USER 65532:65532
ENTRYPOINT ["/northscope"]

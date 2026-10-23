# Stage 1: build a static binary. Stage 2: copy it into a tiny non-root image (no shell, no package manager).
FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/net-healthcheck ./cmd/net-healthcheck

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/net-healthcheck /net-healthcheck
ENTRYPOINT ["/net-healthcheck"]

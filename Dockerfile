# syntax=docker/dockerfile:1

FROM golang:1.27-alpine AS build
RUN apk add --no-cache ca-certificates
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
ARG VERSION=0.1.0
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w -X main.Version=${VERSION}" \
    -o /out/pychat ./cmd/pychat

FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/pychat /pychat
ENTRYPOINT ["/pychat"]

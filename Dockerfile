# syntax=docker/dockerfile:1
ARG GO_VERSION=1.25.9
FROM golang:${GO_VERSION}-bookworm AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .

ARG CLI_VERSION=container
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags "-s -w -X 'github.com/spacetj/noops/cmd.cliVersion=${CLI_VERSION}'" \
    -o /out/noops .

FROM gcr.io/distroless/base-debian12

WORKDIR /app
COPY --from=builder /out/noops /bin/noops

ENV PORT=8080
CMD ["/bin/noops", "serve"]

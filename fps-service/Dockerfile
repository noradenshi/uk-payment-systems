# STAGE 1: Build
FROM golang:1.26-alpine AS builder

RUN apk add --no-cache \
    build-base \
    libxml2-dev \
    libxml2-static \
    zlib-static \
    xz-dev \
    xz-static

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .

RUN CGO_ENABLED=1 GOOS=linux GOARCH=amd64 \
    go build -tags musl \
    -ldflags "-s -w -extldflags '-static -lxml2 -lz -llzma -lpthread -lm'" \
    -o fps-service ./cmd/server/main.go

RUN file fps-service

# STAGE 2: Final
FROM alpine:3.23 AS final
RUN apk add --no-cache ca-certificates libxml2

WORKDIR /app
COPY --from=builder /app/fps-service /usr/local/bin/fps-service
COPY --from=builder /app/xsd ./xsd
RUN chmod +x /usr/local/bin/fps-service

EXPOSE 8081
ENTRYPOINT ["/usr/local/bin/fps-service"]

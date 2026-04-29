# STAGE 1: Build
FROM golang:1.26-alpine AS builder

# Added xz-static and zlib-static
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

# Static build with the sub-dependency chain
RUN CGO_ENABLED=1 GOOS=linux GOARCH=amd64 \
    go build -tags musl \
    -ldflags "-s -w -extldflags '-static -lxml2 -lz -llzma -lpthread -lm'" \
    -o chaps-service ./cmd/server/main.go

# Verify it's finally an ELF executable
RUN file chaps-service

# STAGE 2: Final
FROM alpine:3.23 AS final
RUN apk add --no-cache ca-certificates libxml2

WORKDIR /app
COPY --from=builder /app/chaps-service /usr/local/bin/chaps-service
COPY --from=builder /app/xsd ./xsd
RUN chmod +x /usr/local/bin/chaps-service

EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/chaps-service"]

FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "-s -w" -o bacs-service ./cmd/server/main.go
RUN file bacs-service

FROM alpine:3.23 AS final
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=builder /app/bacs-service /usr/local/bin/bacs-service
RUN chmod +x /usr/local/bin/bacs-service
EXPOSE 8082
ENTRYPOINT ["/usr/local/bin/bacs-service"]

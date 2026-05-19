FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "-s -w" -o fps-service ./cmd/server/main.go
RUN file fps-service

FROM alpine:3.23 AS final
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=builder /app/fps-service /usr/local/bin/fps-service
RUN chmod +x /usr/local/bin/fps-service
EXPOSE 8081
ENTRYPOINT ["/usr/local/bin/fps-service"]

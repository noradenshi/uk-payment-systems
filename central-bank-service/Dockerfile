FROM golang:1.26-alpine AS build

WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/central-bank ./cmd/server

FROM alpine:3.23
WORKDIR /app
COPY --from=build /out/central-bank /app/central-bank
EXPOSE 8090
CMD ["/app/central-bank"]

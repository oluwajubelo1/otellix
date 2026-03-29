FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o /otellix-demo ./examples/docker-compose/app/

FROM alpine:latest
COPY --from=builder /otellix-demo /otellix-demo
EXPOSE 9090
CMD ["/otellix-demo"]

# Must satisfy go.mod's go/toolchain lines: golang images set GOTOOLCHAIN=local.
FROM golang:1.24-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o /otellix-demo ./examples/docker-compose/app/

FROM alpine:latest
COPY --from=builder /otellix-demo /otellix-demo
EXPOSE 9090
CMD ["/otellix-demo"]

FROM golang:1.26-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags='-s -w' \
    -o /out/supasend-to-github ./cmd/server

FROM alpine:3.23

RUN adduser -D -H -u 10001 appuser

WORKDIR /app

COPY --from=builder /out/supasend-to-github /app/supasend-to-github

ENV LISTEN_ADDR=:8080

EXPOSE 8080

USER appuser

ENTRYPOINT ["/app/supasend-to-github"]

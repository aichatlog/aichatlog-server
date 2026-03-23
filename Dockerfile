FROM golang:1.24-alpine AS builder
RUN apk add --no-cache gcc musl-dev
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 CGO_CFLAGS="-DSQLITE_ENABLE_FTS5" go build -o aichatlog-server ./cmd/server

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=builder /app/aichatlog-server .
EXPOSE 4180
VOLUME ["/app/data"]
ENTRYPOINT ["./aichatlog-server"]
CMD ["--port", "4180", "--db", "/app/data/aichatlog.db", "--data", "/app/data/files"]

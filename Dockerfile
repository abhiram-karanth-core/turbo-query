# ---------- build stage ----------
FROM golang:1.25-alpine AS builder

WORKDIR /app

RUN apk add --no-cache gcc musl-dev

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=1 GOOS=linux GOARCH=amd64 \
    go build -o shard-server ./cmd/shard

# ---------- runtime stage ----------
FROM alpine:3.20

WORKDIR /app

RUN apk add --no-cache libc6-compat

COPY --from=builder /app/shard-server .

RUN mkdir -p /data

EXPOSE 8080

CMD ["./shard-server"]
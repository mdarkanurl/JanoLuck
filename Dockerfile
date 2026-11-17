FROM golang:1.27-alpine AS builder

WORKDIR /app

RUN apk add --no-cache build-base

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=1 GOOS=linux go build -tags musl \
   -o /build/server ./cmd/server


RUN CGO_ENABLED=1 GOOS=linux go build -tags musl \
   -o /build/worker ./cmd/worker


FROM alpine:latest AS server

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app
COPY --from=builder /build/server ./server

EXPOSE 3000

CMD ["./server"]


FROM alpine:latest AS worker

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app
COPY --from=builder /build/worker ./worker

CMD ["./worker"]

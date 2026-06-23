FROM golang:1.25-alpine AS builder
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o kms-wrapper ./cmd/kms-wrapper

FROM alpine:3.20
WORKDIR /app
COPY --from=builder /build/kms-wrapper .
ENTRYPOINT ["./kms-wrapper"]
CMD ["serve"]

# Build stage
FROM golang:1.21-alpine AS builder

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the payment service
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o payment-service ./services/payment

# Final stage
FROM alpine:latest

RUN apk --no-cache add ca-certificates tzdata
WORKDIR /root/

# Copy the binary from builder stage
COPY --from=builder /app/payment-service .

# Expose port
EXPOSE 8005

# Run the binary
CMD ["./payment-service"]

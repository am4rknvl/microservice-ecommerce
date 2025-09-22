# Build stage
FROM golang:1.21-alpine AS builder

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the product service
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o product-service ./services/product

# Final stage
FROM alpine:latest

RUN apk --no-cache add ca-certificates tzdata
WORKDIR /root/

# Copy the binary from builder stage
COPY --from=builder /app/product-service .

# Expose port
EXPOSE 8003

# Run the binary
CMD ["./product-service"]

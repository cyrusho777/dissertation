FROM golang:1.22 as builder

WORKDIR /app

# Copy go.mod and go.sum files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy the source code
COPY multi_resource_sched.go .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o multi-resource-scheduler .

# Use a minimal alpine image for the final stage
FROM alpine:3.17

WORKDIR /app

# Copy the binary from the builder stage
COPY --from=builder /app/multi-resource-scheduler .

# Run the application
ENTRYPOINT ["./multi-resource-scheduler"] 
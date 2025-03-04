FROM golang:1.22-alpine AS builder

# Install git and dependencies
RUN apk add --no-cache git gcc musl-dev

WORKDIR /app

# Copy go.mod and go.sum first for better caching
COPY go.mod go.sum* ./

# Force Go to use the older version of sigs.k8s.io/json
RUN go mod download

# Copy the rest of the source code
COPY . .

# Run go mod tidy to ensure dependencies are correct
RUN go mod tidy

# Build the application
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o preemptive-scheduler .

# Use a minimal alpine image for the final container
FROM alpine:3.19

WORKDIR /app
COPY --from=builder /app/preemptive-scheduler .

# Run the scheduler
ENTRYPOINT ["./preemptive-scheduler"] 
# Build stage
FROM golang:1.22 as builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -installsuffix cgo -o multi-resource-scheduler .

# Final image (using a minimal base image)
FROM alpine:3.18
RUN apk --no-cache add ca-certificates
WORKDIR /root/
COPY --from=builder /app/multi-resource-scheduler .
EXPOSE 8888
CMD ["./multi-resource-scheduler"]

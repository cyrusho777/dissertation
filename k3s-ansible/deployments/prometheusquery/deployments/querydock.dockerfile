FROM golang:1.22-alpine
WORKDIR /app
COPY go.mod .
COPY go.sum .
COPY . .
ENV GOARCH=amd64
RUN go mod tidy && go build -o metrics-monitor .
CMD ["./metrics-monitor"]

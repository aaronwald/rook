FROM golang:1.23 as builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o rook .

FROM debian:bullseye-slim
COPY --from=builder /app/rook /app/rook
CMD ["/app/rook",  "/etc/gmail-secret/username", "/etc/gmail-secret/password"]

FROM golang:1.23 as builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o rook .

FROM golang:1.23
COPY --from=builder /app/rook /app/rook
CMD ["/app/rook",  "--gmail-username-file", "/etc/gmail-secret/username", "--gmail-password-file", "/etc/gmail-secret/password", "--mqtt-server", "mqtt", "--mqtt-username", "rook", "--mqtt-password", "rook"]

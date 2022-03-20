FROM golang:1.16-alpine as builder
WORKDIR /build
COPY go.mod .
COPY go.sum .
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /main cmd/app/main.go
FROM alpine:3
COPY --from=builder main /bin/main
ENTRYPOINT ["/bin/main"]
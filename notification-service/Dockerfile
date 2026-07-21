FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/notification-service ./cmd/notification-service

FROM alpine:3.20
RUN adduser -D -H appuser
COPY --from=build /out/notification-service /usr/local/bin/notification-service
USER appuser
EXPOSE 8083
ENTRYPOINT ["/usr/local/bin/notification-service"]

FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/order-service ./cmd/order-service

FROM alpine:3.20
RUN adduser -D -H appuser
COPY --from=build /out/order-service /usr/local/bin/order-service
USER appuser
EXPOSE 8082
ENTRYPOINT ["/usr/local/bin/order-service"]

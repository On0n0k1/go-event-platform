FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/inventory-service ./cmd/inventory-service

FROM alpine:3.20
RUN adduser -D -H appuser
COPY --from=build /out/inventory-service /usr/local/bin/inventory-service
USER appuser
EXPOSE 8081
ENTRYPOINT ["/usr/local/bin/inventory-service"]

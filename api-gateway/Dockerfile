FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/api-gateway ./cmd/api-gateway

FROM alpine:3.20
RUN adduser -D -H appuser
COPY --from=build /out/api-gateway /usr/local/bin/api-gateway
USER appuser
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/api-gateway"]

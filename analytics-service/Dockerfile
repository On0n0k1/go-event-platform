FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/analytics-service ./cmd/analytics-service

FROM alpine:3.20
RUN adduser -D -H appuser
COPY --from=build /out/analytics-service /usr/local/bin/analytics-service
USER appuser
EXPOSE 8084
ENTRYPOINT ["/usr/local/bin/analytics-service"]

FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG TARGET=./services/gateway/cmd
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/app ${TARGET} \
    && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/healthcheck ./cmd/healthcheck

FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata && adduser -D -u 10001 app
WORKDIR /app
COPY --from=build /out/app /out/healthcheck ./
COPY migrations ./migrations
COPY docs/openapi.yaml ./docs/openapi.yaml
USER app
ENTRYPOINT ["/app/app"]

# Build context is the repo root (see docker-compose.yml) so ./shared is available.
FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY shared ./shared
COPY timer ./timer
RUN CGO_ENABLED=0 go build -o /out/app ./timer

FROM alpine:3.20
RUN apk add --no-cache wget
COPY --from=build /out/app /app
EXPOSE 8003
ENTRYPOINT ["/app"]

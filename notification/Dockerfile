FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /notification ./cmd/notification

FROM alpine:3.20
RUN adduser -D -H app
COPY --from=build /notification /notification
USER app
EXPOSE 8005
ENTRYPOINT ["/notification"]

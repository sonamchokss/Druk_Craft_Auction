FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY *.go ./
RUN go mod tidy && CGO_ENABLED=0 go build -o /listing .

FROM alpine:3.20
COPY --from=build /listing /listing
EXPOSE 8001
CMD ["/listing"]
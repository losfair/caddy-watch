FROM golang:1.17.3-alpine
RUN apk add librdkafka
COPY . /app
WORKDIR /app
RUN go build -o caddy-watch ./main.go

FROM alpine:latest
RUN apk add librdkafka
COPY --from=0 /app/caddy-watch /usr/bin/
ENTRYPOINT ["/usr/bin/caddy-watch"]

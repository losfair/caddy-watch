FROM golang:1.17.3-bullseye
RUN apt update && apt install -y build-essential
COPY . /app
WORKDIR /app
RUN go build -o caddy-watch -tags static ./main.go

FROM debian:bullseye-slim
COPY --from=0 /app/caddy-watch /usr/bin/
ENTRYPOINT ["/usr/bin/caddy-watch"]

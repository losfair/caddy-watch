# caddy-watch

Watch for interesting patterns in Caddy logs and send a Telegram notification.

This should be used with [caddy-log-kafka](https://github.com/losfair/caddy-log-kafka).

## Usage

1. Create a config file `caddy-watch.yaml`:

```yaml
interests:
- name: private-apps
  description: First 2xx response for each (remote_ip, host) pair from private apps
  matcher:
    any:
    - host: privapp1.example.com
    - host: privapp2.example.com
    status:
      min: 200
      max: 299
  suppressDurationSecs: 300
- name: secret-data
  description: URI pattern match
  matcher:
    host: data.example.com
    uriPattern: '^\/secret\/.*$'
telegram:
  toChat: 0 # Replace with your Telegram chat id
```


2. Start the service:

```
docker run -it --rm \
  -e TELEGRAM_BOT_TOKEN="<put your telegram bot token here>" \
  ghcr.io/losfair/caddy-watch \
  -bootstrap-servers localhost:9093 \
  -config ./caddy-watch.yaml \
  -group-id com.example.caddy-watch.default \
  -topics com.example.caddy.default
```

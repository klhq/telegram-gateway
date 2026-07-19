<p align="center">
  <img src="assets/tg-gateway.png" alt="Telegram Gateway Logo" width="160" height="160" />
</p>

# Telegram Gateway

A lightweight, high-performance, and generalized Telegram Gateway in Go. It acts as a single inbound and outbound messaging hub for multiple independent downstream service daemons sharing a single Telegram Bot token.

## Why this is needed

Multiple processes cannot run `getUpdates` concurrently using the same bot token without conflicting. The Telegram Gateway solves this by:
1. Long-polling `getUpdates` from Telegram as the sole receiver.
2. Routing callback queries dynamically to the appropriate local backend services based on custom payload prefixes.
3. Enforcing client-side rate limits (global and per-chat) to avoid hitting Telegram's limits.
4. Securing callback forwards using HMAC-SHA256 signatures.
5. Providing a unified `/send` endpoint for all strategies to dispatch alerts.

---

## Architecture

```
                                  +-----------------------+
                                  |     Telegram API      |
                                  +-----------+-----------+
                                     ^        |
                              /send  |        | getUpdates
                              (POST) |        v (Long-polling)
                                  +-----------+-----------+
                                  |                       |
                                  |   Telegram Gateway    |
                                  |                       |
                                  +-----+-----------+-----+
                                        |           |
             [prefix1]:* callback (POST)|           | [prefix2]:* callback (POST)
                                        v           v
                            +-----------+---+   +---+-----------+
                            |   Service A   |   |   Service B   |
                            +---------------+   +---------------+
```

---

## Configuration

Configuration is loaded from a JSON file. By default, it looks for `config.json` in the current working directory, or you can specify a custom path via the `CONFIG_PATH` environment variable or the `-config` flag.

### Configuration Fields (`config.json`)

| Field | Type | Description | Default |
|---|---|---|---|
| `telegram_bot_token` | String | **Required.** Your Telegram Bot API token. | N/A |
| `telegram_chat_id` | Integer | Optional default target chat ID. | `0` |
| `port` | String | Port for the gateway server to listen on. | `8000` |
| `gateway_api_key` | String | Optional shared secret for token-based authentication on `POST /send`. | N/A |
| `webhook_secret` | String | Optional shared secret key for signing forwarded callback webhooks (HMAC-SHA256). | N/A |
| `log_level` | String | Log verbosity filter: `DEBUG`, `INFO`, `WARN`, `ERROR`. | `INFO` |
| `routes` | Map | Dynamic mapping of payload prefixes to target backend URLs. | `{}` |
| `rate_limits.global_per_second` | Float | Maximum messages allowed bot-wide per second. | `30.0` |
| `rate_limits.chat_per_second` | Float | Maximum messages allowed per individual chat per second. | `1.0` |

Example `config.json`:
```json
{
  "telegram_bot_token": "123456789:ABCdefGhIJKlmNoPQRsTUVwxyZ",
  "telegram_chat_id": 123456789,
  "port": "8000",
  "gateway_api_key": "some-secure-key",
  "webhook_secret": "my-signing-secret",
  "log_level": "INFO",
  "routes": {
    "receiver-a": "http://localhost:8001/callback",
    "receiver-b": "http://localhost:8002/callback"
  },
  "rate_limits": {
    "global_per_second": 30.0,
    "chat_per_second": 1.0
  }
}
```

---

## Getting Started

### Prerequisites
- Go 1.21+ (built and tested on Go 1.26)
- Docker & Docker Compose (optional)

### Running Locally
1. Copy the example configuration and fill in your Bot Token:
   ```bash
   cp config.json.example config.json
   ```
2. Start the gateway:
   ```bash
   go run .
   ```
   *Note: If no `gateway_api_key` is set, the gateway will fail to start by default unless the environment variable `INSECURE_DEV_MODE=true` is set.*

### Running via Docker Compose
To boot up a local developer stack featuring the gateway, two mock receiver services, and Prometheus:
```bash
docker compose up --build
```
This is configured out-of-the-box using the parameters in [docker/config.json](file:///home/klhsu/workspace/projects/telegram-gateway/docker/config.json).

---

## API Endpoints

### 1. Send Message (`POST /send`)
Allows downstream services to send a message to a specific chat. If `gateway_api_key` is configured, client requests must supply the key: `Authorization: Bearer <gateway_api_key>`.

#### Request Schema
* `chat_id` (Integer): Target chat. Defaults to `telegram_chat_id` if omitted.
* `text` (String): **Required.** Message content.
* `parse_mode` (String): Formatting style: `Markdown`, `HTML`, `MarkdownV2`. Default: `Markdown`.
* `disable_web_page_preview` (Boolean): Suppresses link webpage previews if `true`.
* `disable_notification` (Boolean): Delivers the message silently (no sound/vibe) if `true`.
* `reply_markup` (Object): InlineKeyboardMarkup definition.

#### Request Example
```bash
curl -X POST http://localhost:8000/send \
  -H "Authorization: Bearer some-secure-key" \
  -H "Content-Type: application/json" \
  -d '{
    "chat_id": 123456789,
    "text": "🚨 *ALERT*: Action detected! Link: https://example.com",
    "disable_web_page_preview": true,
    "disable_notification": true
  }'
```

#### Response Example
```json
{
  "message_id": 999,
  "chat_id": 123456789
}
```

---

### 2. Inbound Callback Forwarding (Gateway -> Downstream)
When a user interacts with inline keyboard buttons, the gateway routes the callback query to the appropriate service based on prefix mapping.

#### Header Signature Validation
If `webhook_secret` is set, the gateway signs the payload using HMAC-SHA256 and attaches the signature hex-string under:
`X-Gateway-Signature: <hex_signature>`

Downstream clients must compute the HMAC of the raw request body using the shared secret and perform constant-time comparison to verify payload authenticity and integrity.

#### Forwarded Payload Shape
```json
{
  "callback_query_id": "1234567890123456",
  "from_id": 987654321,
  "username": "developer_user",
  "chat_id": 123456789,
  "message_id": 999,
  "data": "receiver-a:approve:ev1"
}
```

---

### 3. Prometheus Metrics (`GET /metrics`)
Exposes telemetry indicators for scrapers (e.g. Prometheus):
* `telegram_gateway_incoming_updates_total`: Incoming updates from Telegram.
* `telegram_gateway_callback_forward_total`: Forwarded callback metrics.
* `telegram_gateway_callback_forward_duration_seconds`: Hook latency histogram.
* `telegram_gateway_send_requests_total`: Outbound dispatch request metrics.

---

### 4. Health Check (`GET /health`)
Returns `200 OK`:
```json
{"status":"ok"}
```

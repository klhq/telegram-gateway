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

The gateway reads `config.json` from its working directory. Set `CONFIG_PATH` or pass `-config` to use another path. The public contract lives in [`config.schema.json`](config.schema.json).

Container deployments keep routes and rate limits in `config.json`. The standard Compose example reads the three secrets from a local `.env` file.

### Configuration Fields (`config.json`)

| Field | Type | Description | Default |
|---|---|---|---|
| `telegram_bot_token` | String | Legacy inline bot token. One token source is required after configuration sources are applied. | N/A |
| `telegram_chat_id` | Integer | Optional default target chat ID. | `0` |
| `port` | String | Port for the gateway server to listen on. | `8000` |
| `gateway_api_key` | String | Legacy inline bearer token for `POST /send`. Secure mode requires one API key source. | N/A |
| `webhook_secret` | String | Optional shared secret key for signing forwarded callback webhooks (HMAC-SHA256). | N/A |
| `log_level` | String | Log verbosity filter: `DEBUG`, `INFO`, `WARN`, `ERROR`. | `INFO` |
| `routes` | Map | Dynamic mapping of payload prefixes to target backend URLs. | `{}` |
| `rate_limits.global_per_second` | Float | Maximum messages allowed bot-wide per second. | `30.0` |
| `rate_limits.chat_per_second` | Float | Maximum messages allowed per individual chat per second. | `1.0` |

Example `config.json`:
```json
{
  "telegram_chat_id": 123456789,
  "port": "8000",
  "log_level": "INFO",
  "routes": {
    "strategy-a": "http://strategy-a:8080/telegram/callback"
  },
  "rate_limits": {
    "global_per_second": 30.0,
    "chat_per_second": 1.0
  }
}
```

### Environment and secret sources

| Variable | Purpose |
|---|---|
| `TELEGRAM_BOT_TOKEN` | Bot token value. |
| `TELEGRAM_BOT_TOKEN_FILE` | File containing the bot token. Overrides `TELEGRAM_BOT_TOKEN` and `telegram_bot_token`. |
| `GATEWAY_API_KEY` | Bearer token accepted by `POST /send`. |
| `GATEWAY_API_KEY_FILE` | File containing the gateway API key. Overrides `GATEWAY_API_KEY` and `gateway_api_key`. |
| `WEBHOOK_SECRET` | HMAC key used to sign forwarded callbacks. |
| `WEBHOOK_SECRET_FILE` | File containing the callback HMAC key. Overrides `WEBHOOK_SECRET` and `webhook_secret`. |
| `ROUTES_JSON` | JSON object containing the complete callback route map. |
| `ROUTES_FILE` | Path to a JSON file containing the complete callback route map. |
| `TELEGRAM_CHAT_ID` | Overrides `telegram_chat_id`. |
| `PORT` | Overrides `port`. |
| `LOG_LEVEL` | Overrides `log_level`. |
| `GLOBAL_RATE_LIMIT` | Overrides `rate_limits.global_per_second`. |
| `CHAT_RATE_LIMIT` | Overrides `rate_limits.chat_per_second`. |

The loader applies sources in this order:

1. Read `config.json` as the base configuration.
2. Apply direct environment variables.
3. Read `*_FILE` secrets, which take precedence over direct secret variables.
4. Apply `ROUTES_JSON` or `ROUTES_FILE`, replacing the route map from `config.json`.

Set either `ROUTES_JSON` or `ROUTES_FILE`. Startup fails if you set both. The legacy `COMB_ARB_URL` and `BOOK_ARB_URL` variables remain supported; a generic route source replaces them.

---

## Getting Started

### Prerequisites
- Go 1.26+ for source builds
- Docker & Docker Compose (optional)

### Running Locally
1. Copy the non-secret configuration:
   ```bash
   cp config.json.example config.json
   ```
2. Supply secrets and start the gateway:
   ```bash
   export TELEGRAM_BOT_TOKEN='<bot-token>'
   export GATEWAY_API_KEY='<gateway-api-key>'
   export WEBHOOK_SECRET='<callback-signing-secret>'
   go run .
   ```

The gateway refuses to start without `GATEWAY_API_KEY`. Set `INSECURE_DEV_MODE=true` only for an isolated development environment.

### Running the published image

Download `compose.example.yaml`, `.env.example`, and `config.json.example` from this repository (or its release bundle) into an empty deployment directory, then create the local files:

```bash
cp compose.example.yaml compose.yaml
cp .env.example .env
cp config.json.example config.json
chmod 600 .env
```

Edit `.env` and set `TELEGRAM_BOT_TOKEN`, `GATEWAY_API_KEY`, and `WEBHOOK_SECRET`. Set `TELEGRAM_GATEWAY_IMAGE` to a release tag before deploying to a long-lived server. Edit `config.json` to add your strategy routes.

Start the gateway:

```bash
docker compose pull
docker compose up -d
```

The example binds the gateway to `127.0.0.1:8000`. Change the port mapping or attach an internal Docker network when another host or container must call `/send`.

Edit `config.json` to add or change callback routes, then restart the gateway because it reads configuration during startup:

```bash
docker compose restart gateway
```

To upgrade, change `TELEGRAM_GATEWAY_IMAGE` in `.env`, then recreate the service:

```bash
docker compose pull
docker compose up -d
```

### Secret manager deployments

The gateway also accepts `TELEGRAM_BOT_TOKEN_FILE`, `GATEWAY_API_KEY_FILE`, and `WEBHOOK_SECRET_FILE`. Use those variables when Komodo, Kubernetes, CI/CD, or another deployment platform mounts secrets as files. File-backed values override the matching values in `.env`.

### Running the development stack

The repository Compose file builds the current checkout and starts two mock receivers plus Prometheus:

```bash
docker compose up --build
```

It uses [`docker/config.json`](docker/config.json).

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

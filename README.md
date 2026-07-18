# Telegram Gateway

A lightweight, high-performance, and generalized Telegram Gateway in Go. It acts as a single inbound and outbound messaging hub for multiple independent downstream service daemons sharing a single Telegram Bot token.

## Why this is needed

Multiple processes cannot run `getUpdates` concurrently using the same bot token without conflicting. The Telegram Gateway solves this by:
1. Long-polling `getUpdates` from Telegram as the sole receiver.
2. Routing callback queries dynamically to the appropriate local backend services based on custom payload prefixes.
3. Providing a unified `POST /send` endpoint for all strategies to send messages/alerts out to users.
4. Exposing Prometheus metrics for full observability.

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

| Field | Type | Description |
|---|---|---|
| `telegram_bot_token` | String | **Required.** Your Telegram Bot API token. |
| `telegram_chat_id` | Integer | Optional default target chat ID. |
| `port` | String | Port for the gateway server to listen on. Default: `8000`. |
| `gateway_api_key` | String | Optional shared secret for token-based authentication on `POST /send`. |
| `routes` | Map (String -> String) | Dynamic mapping of payload prefixes to target backend URLs. |

Example `config.json`:
```json
{
  "telegram_bot_token": "123456789:ABCdefGhIJKlmNoPQRsTUVwxyZ",
  "telegram_chat_id": 123456789,
  "port": "8000",
  "gateway_api_key": "some-secure-key",
  "routes": {
    "combo": "http://localhost:8001/callback",
    "book": "http://localhost:8002/callback"
  }
}
```

---

## Getting Started

### Prerequisites
- Go 1.21+ (built and tested on Go 1.26)

### Installation
Clone the repository and install the dependencies:
```bash
go mod download
```

### Running the Gateway
1. Create a `config.json` file in the root directory (see example above).
2. Start the gateway:
   ```bash
   go run .
   ```
   Or explicitly pass a config path:
   ```bash
   go run . -config /path/to/config.json
   ```

### Running the Tests
To run the unit/integration tests:
```bash
go test -v ./...
```

---

## API Endpoints

### 1. Send Message (`POST /send`)
Allows strategies to send a message to a specific chat (optionally with an inline keyboard and markdown formatting).

If `gateway_api_key` is set, calls must include the key in the authorization header: `Authorization: Bearer <gateway_api_key>`.

#### Request Example
```bash
curl -X POST http://localhost:8000/send \
  -H "Authorization: Bearer some-secure-key" \
  -H "Content-Type: application/json" \
  -d '{
    "chat_id": 123456789,
    "text": "🚨 *COMB-ARB ALERT*: Spain vs. Cabo Verde...",
    "reply_markup": {
      "inline_keyboard": [
        [
          {"text": "🟢 Approve", "callback_data": "combo:approve:ev1"},
          {"text": "🔴 Decline", "callback_data": "combo:decline:ev1"}
        ]
      ]
    }
  }'
```

#### Response Example (Success)
```json
{
  "message_id": 999,
  "chat_id": 123456789
}
```

---

### 2. Inbound Callback Forwarding (Gateway -> Strategy)
When the user clicks an inline button, the gateway forwards the callback query to the designated service based on the matched prefix (e.g. `combo:*` forwards to the URL configured for the `combo` prefix).

#### Forwarded Payload Format
The gateway forwards a flattened JSON structure:
```json
{
  "callback_query_id": "1234567890123456",
  "from_id": 987654321,
  "username": "example_user",
  "chat_id": 123456789,
  "message_id": 999,
  "data": "combo:approve:ev1"
}
```

#### Strategy Response Expectations
The strategy service should respond with `200 OK` and can optionally return a JSON payload to customize the Telegram callback answer:
```json
{
  "text": "Approved successfully!",
  "show_alert": false
}
```

---

### 3. Prometheus Metrics (`GET /metrics`)
Exposes standard Prometheus-compatible telemetry metrics for monitoring.
* `telegram_gateway_incoming_updates_total`: Count of incoming Telegram updates (labeled by update type).
* `telegram_gateway_callback_forward_total`: Total callbacks forwarded to strategy backends (labeled by prefix and status).
* `telegram_gateway_callback_forward_duration_seconds`: Latency of callback query forwarding to backend strategies.
* `telegram_gateway_send_requests_total`: Total POST `/send` requests (labeled by HTTP status code).

---

### 4. Health Check (`GET /health`)
Returns `200 OK` health status:
```json
{"status":"ok"}
```


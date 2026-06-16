# Telegram Gateway

A lightweight, high-performance Telegram Gateway in Go. It acts as a single inbound and outbound messaging hub for multiple independent trading strategy daemons (e.g., `polymarket-comb-arb`, `polymarket-book-arb`) sharing a single Telegram Bot token.

## Why this is needed

Multiple processes cannot run `getUpdates` concurrently using the same bot token without conflicting. The Telegram Gateway solves this by:
1. Long-polling `getUpdates` from Telegram as the sole receiver.
2. Routing callback queries to the appropriate local strategy service based on the payload prefix (`combo:` or `book:`).
3. Providing a unified `POST /send` endpoint for all strategies to send messages/alerts out to users.

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
            combo:* callback (POST)    |           |    book:* callback (POST)
                                        v           v
                    +-------------------+---+   +---+-------------------+
                    |  polymarket-comb-arb  |   |  polymarket-book-arb  |
                    | (default: port 8001)  |   | (default: port 8002)  |
                    +-----------------------+   +-----------------------+
```

---

## Configuration

Configuration is loaded from environment variables (or a `.env` file in the root directory).

| Variable | Description | Default |
|---|---|---|
| `TELEGRAM_BOT_TOKEN` | **Required.** Your Telegram Bot API token. | N/A |
| `PORT` | Port for the gateway HTTP server to listen on. | `8000` |
| `COMB_ARB_URL` | Destination URL for `combo:` prefix callback queries. | `http://localhost:8001/callback` |
| `BOOK_ARB_URL` | Destination URL for `book:` prefix callback queries. | `http://localhost:8002/callback` |

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
1. Create a `.env` file in the root directory:
   ```env
   TELEGRAM_BOT_TOKEN=123456789:ABCdefGhIJKlmNoPQRsTUVwxyZ
   PORT=8000
   COMB_ARB_URL=http://localhost:8001/callback
   BOOK_ARB_URL=http://localhost:8002/callback
   ```

2. Start the gateway:
   ```bash
   go run .
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

#### Request Example
```bash
curl -X POST http://localhost:8000/send \
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

#### Response Example (Error)
```json
{
  "error": "Bad Request: chat not found"
}
```

---

### 2. Inbound Callback Forwarding (Gateway -> Strategy)
When the user clicks an inline button, the gateway forwards the callback query to the designated service based on the `data` prefix:

- `combo:*` -> `COMB_ARB_URL`
- `book:*`  -> `BOOK_ARB_URL`

#### Forwarded Payload Format
The gateway forwards a flattened JSON structure to the strategy:
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
The strategy service should respond with `200 OK` and can optionally return a JSON payload to customize the Telegram callback answer (e.g., displaying a toast notification to the user):

```json
{
  "text": "Approved successfully!",
  "show_alert": false
}
```
*If the strategy returns an empty response body or an invalid JSON, the gateway automatically acknowledges the callback with a default success state.*

#### Fallback Behavior
If the strategy service is down or fails to respond within **5 seconds**, the gateway will intercept the timeout/failure and trigger `answerCallbackQuery` with:
- `text`: `"Strategy backend unreachable"`
- `show_alert`: `true` (renders as a pop-up alert in the client)
This prevents the user's client from hanging on a spinner.

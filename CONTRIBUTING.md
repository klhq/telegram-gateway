# Contributing to Telegram Gateway

Thank you for your interest in contributing to the Telegram Gateway project! Please follow these guidelines to set up your environment, write clean code, and submit contributions.

---

## Getting Started

### Prerequisites
* **Go**: version 1.21+ (built and verified on 1.26)
* **Docker** & **Docker Compose** (optional, for running local integration environment)

### Clone the Repository
```bash
git clone <repository_url>
cd telegram-gateway
```

---

## Local Development & Testing

### Running Tests
To run all tests locally (with formatting checks and race detection enabled):
```bash
go test -v -race ./...
```

To vet the code for common issues:
```bash
go vet ./...
```

### Code Formatting
Ensure all Go files conform to standard formatting before submitting a pull request:
```bash
gofmt -w .
```

---

## Using the Local Docker-Compose Stack

We provide an out-of-the-box local development container stack that spins up:
1. **Telegram Gateway** listening on port `8000`.
2. **Strategy A** (mock webhook handler) listening on port `8081`.
3. **Strategy B** (mock webhook handler) listening on port `8082`.
4. **Prometheus** listening on port `9090` (configured to scrape gateway metrics).

### How to Run:
1. Start the stack:
   ```bash
   docker compose up --build
   ```
2. Send a mock request to `/send`:
   ```bash
   curl -X POST http://localhost:8000/send \
     -H "Authorization: Bearer dev-secret-key" \
     -H "Content-Type: application/json" \
     -d '{
       "chat_id": 123456789,
       "text": "Hello strategy-a!",
       "disable_notification": true
     }'
   ```
3. Access Prometheus at `http://localhost:9090` to view gateway scrape metrics.

---

## Coding Principles

1. **Secure by Default**:
   * Never hardcode secrets. Always load them dynamically from the JSON configuration.
   * Endpoints modifying state or sending actions (like `/send`) must remain authenticated.
2. **Thread Safety**:
   * The update polling loop dispatches incoming updates in background goroutines. Ensure all shared variables or maps are protected using synchronization primitives (e.g. `sync.Mutex`, `sync.RWMutex`, or thread-safe registries).
3. **No Unused Code**:
   * Keep external dependencies minimal. Avoid pulling in large frameworks if standard library code is sufficient.

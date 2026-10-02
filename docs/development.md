# Development from source

Normal users do not need to clone the repository.

```bash
git clone https://github.com/boboty/agent-board.git
cd agent-board
go build -o aboard ./cmd/aboard
go test ./...
```

Real-browser e2e tests live in a separate Go module:

```bash
cd e2e
go test ./...
```

See [AGENTS.md](../AGENTS.md), [PRODUCT.md](../PRODUCT.md), and [ARCHITECTURE.md](../ARCHITECTURE.md) before making product or workflow changes.

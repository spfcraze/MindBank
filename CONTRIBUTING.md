# Contributing to MindBank

Thanks for your interest in contributing to MindBank!

## Getting Started

1. Fork the repository
2. Clone your fork: `git clone https://github.com/YOUR_USERNAME/MindBank.git`
3. Copy config: `cp .env.example .env`
4. Start services: `make run`
5. Verify: `make health`

## Development Workflow

```bash
# Make your changes
vim internal/handler/yourfile.go

# Run checks
make vet          # Static analysis
make test         # Run tests

# Build and test locally
make run
curl http://localhost:8095/api/v1/health
```

## Code Standards

- **Go**: Follow standard Go conventions. Use `go vet` before committing.
- **Error handling**: Always handle errors explicitly. Use `fmt.Errorf("context: %w", err)` for wrapping.
- **SQL**: Use parameterized queries (`$1, $2`). Never concatenate user input into SQL.
- **Logging**: Use `slog` for structured logging, not `log.Printf`.
- **Tests**: Add tests for new endpoints. Use the benchmarks/ test patterns.

## Adding a New Endpoint

1. Add the handler in `internal/handler/`
2. Register the route in `internal/handler/router.go`
3. Add any repository methods in `internal/repository/`
4. Add models in `internal/models/`
5. Write tests in `benchmarks/`
6. Update `docs/API.md`

## Adding a New Node Type or Edge Type

1. Add the constant to `internal/models/node.go` or `internal/models/edge.go`
2. If it's an edge type, also add to the PostgreSQL enum in a migration
3. Update the MCP tool schema in `internal/mcp/server.go`
4. Update the README

## Pull Requests

- Keep PRs focused on a single change
- Include a clear description of what and why
- Reference any related issues
- Ensure `make vet` and `make test` pass

## Reporting Issues

- Use GitHub Issues
- Include: OS, Go version, steps to reproduce, expected vs actual behavior
- For bugs, include the `/api/v1/health` output

## License

By contributing, you agree that your contributions will be licensed under the MIT License.

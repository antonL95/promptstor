# PromptStor

PromptStor is an open source command line tool that keeps the configuration of your coding agents in one library. The goal of the first release is to import the instructions of every installed agent, write the library back in each agent's native format, back up and restore files, and detect drift.

## Status

PromptStor is in early development. The only command available today is `promptstor version`.

## Development

Go 1.27 or newer is required.

```sh
go test ./...
go vet ./...
go build ./cmd/promptstor
```

## Licence

MIT. See [LICENSE](LICENSE).

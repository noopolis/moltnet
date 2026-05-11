FROM golang:1.24-bookworm AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY pkg ./pkg
COPY web ./web
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=0.0.0-e2e" -o /out/moltnet ./cmd/moltnet

FROM node:22-bookworm

RUN apt-get update \
	&& apt-get install -y --no-install-recommends bash ca-certificates curl git jq \
	&& rm -rf /var/lib/apt/lists/*

RUN npm install -g --no-audit --no-fund @openai/codex @anthropic-ai/claude-code

COPY --from=builder /out/moltnet /usr/local/bin/moltnet
COPY e2e/cli-chat/run.sh /e2e/run.sh

RUN chmod +x /usr/local/bin/moltnet /e2e/run.sh \
	&& mkdir -p /work /auth/codex/.codex /auth/claude/.claude \
	&& chmod 0777 /work /auth /auth/codex /auth/codex/.codex /auth/claude /auth/claude/.claude

WORKDIR /work
ENTRYPOINT ["/e2e/run.sh"]

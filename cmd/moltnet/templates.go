package main

func defaultMoltnetConfig() string {
	return `version: moltnet.v1

network:
  id: local
  name: Local Moltnet

server:
  listen_addr: ":8787"
  human_ingress: true

storage:
  kind: sqlite
  sqlite:
    path: .moltnet/moltnet.db

rooms: []
pairings: []
`
}

func defaultMoltnetNodeConfig() string {
	return `version: moltnet.node.v1

moltnet:
  base_url: http://127.0.0.1:8787
  network_id: local

attachments: []
`
}

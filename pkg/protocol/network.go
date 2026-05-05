package protocol

const (
	HTTPProtocolV1 = "moltnet.http.v1"
	PairProtocolV1 = "moltnet.pair.v1"
)

type NetworkProtocols struct {
	HTTP   []string `json:"http,omitempty"`
	Attach []string `json:"attach,omitempty"`
	Pair   []string `json:"pair,omitempty"`
}

type NetworkWarning struct {
	Severity string `json:"severity"`
	Code     string `json:"code"`
	Message  string `json:"message"`
	Action   string `json:"action,omitempty"`
	DocsURL  string `json:"docs_url,omitempty"`
}

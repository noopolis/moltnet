package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"syscall"
	"time"

	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/pkg/protocol"
)

// PLAN.md 7C.1, open question 2 (DECIDED): `moltnet status` is the
// composite -- network health, participants, peers, warnings, and daemon
// state -- while `moltnet service status` (service.go) keeps its existing
// narrow "is the daemon running" answer unchanged. There is deliberately no
// `network status` and no separate `metrics` verb: metrics folds in behind
// --verbose (statusMetricsView, below).
func runStatus(args []string) error {
	flags := flag.NewFlagSet("moltnet status", flag.ContinueOnError)
	flags.SetOutput(stdout)

	var (
		configPath = flags.String("config", "", "explicit Moltnet client config path")
		memberID   = flags.String("member", "", "Moltnet member id when a network has multiple attachments")
		networkID  = flags.String("network", "", "Moltnet network id when multiple attachments are configured")
		verbose    = flags.Bool("verbose", false, "also fetch /metrics (requires a credential with the admin scope)")
	)
	override := bindOperatorOverrideFlags(flags)

	// Mirrors read.go's runRead: every flag.FlagSet here is built with
	// flags.SetOutput(stdout), so "-h"/"--help" already print usage via
	// Go's flag package and return flag.ErrHelp before this check ever
	// runs; this only catches the bare word "help" as an argument.
	if len(args) > 0 && args[0] == "help" {
		flags.Usage()
		return nil
	}
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("status does not accept positional arguments")
	}

	// A credential that fails to resolve at all -- no client config
	// anywhere and no usable local server config/token either
	// (resolveOperatorClient's own errors already name the exact remedy,
	// e.g. "run `moltnet init` first") is reported via the same clean,
	// actionable error every other command already returns here; there is
	// nothing for `status` to report without knowing where to even ask.
	// This is PLAN.md 7C.1's "handle gracefully ... a credential cannot be
	// resolved" requirement: no panic, no raw dump, just that one message.
	attachment, client, _, err := resolveClientOrOperator(*configPath, *networkID, *memberID, *override, authn.ScopeObserve)
	if err != nil {
		return err
	}

	ctx := commandContext()
	view := statusView{NetworkID: attachment.NetworkID, BaseURL: attachment.BaseURL}

	// The daemon-down case: a resolved credential says nothing about
	// whether the server process is actually up. Reachability is checked
	// with a direct, unauthenticated /healthz probe (anonymously reachable
	// in every auth mode, internal/transport/http.go) before any of the
	// scoped calls below, so a connection-refused/timeout error is
	// collapsed into one clean "not running" report instead of leaking a
	// raw dial error through client.GetNetwork's wrapped transport error.
	reachable, healthy := probeStatusHealth(ctx, attachment.BaseURL)
	view.Daemon = statusDaemonView{Running: reachable, Healthy: healthy}
	if !reachable {
		view.Daemon.Detail = fmt.Sprintf(
			"server did not answer at %s; run `moltnet service start` (or `moltnet service install`, if none is installed yet) for this network",
			attachment.BaseURL,
		)
		// Mirrors runServiceStatus's own convention (service.go): reporting
		// "not running" is this command doing its job correctly, not a CLI
		// failure, so this returns nil (exit 0) rather than an error.
		return printJSON(view)
	}

	network, err := client.GetNetwork(ctx)
	if err != nil {
		view.NetworkError = err.Error()
	} else {
		view.Network = &network
		view.Warnings = network.Warnings
	}

	agentPage, err := client.ListAgents(ctx, protocol.PageRequest{})
	if err != nil {
		view.ParticipantsError = err.Error()
	} else {
		view.Participants = agentPage.Agents
		view.ParticipantsTruncated = agentPage.Page.HasMore
	}

	pairingPage, err := client.ListPairings(ctx, protocol.PageRequest{})
	if err != nil {
		view.PeersError = err.Error()
	} else {
		view.Peers = pairingPage.Pairings
		view.PeersTruncated = pairingPage.Page.HasMore
	}

	if *verbose {
		attachStatusMetrics(ctx, &view, *configPath, *networkID, *memberID, *override)
	}

	return printJSON(view)
}

// attachStatusMetrics resolves a second, independently-scoped client for
// /metrics (admin-scoped server-side, unlike the rest of status's
// observe-scoped legs) and fills view.Metrics or view.MetricsError.
//
// Calling resolveClientOrOperator again with authn.ScopeAdmin is only ever
// meaningful for the zero-setup operator fallback
// (resolveOperatorClient/selectLeastPrivilegedToken, client_operator_fallback.go),
// which actually picks a different token per requested scope; the
// client-config path (resolveClientForMember) and the raw --base-url
// override both ignore requiredScope entirely and resolve to the exact same
// client either way, so calling this unconditionally is always correct, not
// just for the fallback case: no admin-scoped credential simply means
// view.MetricsError gets set and every other section of the report still
// prints, exactly like a failed pairings or agents leg does above.
func attachStatusMetrics(
	ctx context.Context,
	view *statusView,
	configPath string,
	networkID string,
	memberID string,
	override operatorOverrideOptions,
) {
	_, metricsClient, _, err := resolveClientOrOperator(configPath, networkID, memberID, override, authn.ScopeAdmin)
	if err != nil {
		view.MetricsError = err.Error()
		return
	}

	raw, err := metricsClient.Metrics(ctx)
	if err != nil {
		view.MetricsError = err.Error()
		return
	}

	metrics := parseStatusMetrics(raw)
	view.Metrics = &metrics
}

// statusHealthProbeTimeout bounds probeStatusHealth's dial+response wait so
// a hung or firewalled address never makes `status` itself hang.
const statusHealthProbeTimeout = 3 * time.Second

var statusHealthHTTPClient = &http.Client{Timeout: statusHealthProbeTimeout}

// probeStatusHealth reports whether baseURL's server answers at all
// (reachable) and, if so, whether GET /healthz returned 200 (healthy).
// /healthz is anonymousAllowedInOpen in every auth mode
// (internal/transport/http.go), so this never needs attachment's resolved
// token -- only its address -- exactly like console.go's own
// probeConsoleHealth, duplicated narrowly here rather than shared across
// packages so status.go carries no dependency on console.go's own evolution.
//
// F3 (review round 2, confirmed live): reachable must mean "definitely not
// running", not merely "the probe request did not succeed". Both callers of
// this function treat reachable == false as license to report "no server
// running" -- runStatus prints a clean daemon-down report, and
// pair_revoke.go's printPairRevokeAftercare skips its "a server is still
// running and still trusts the just-revoked credential" warning entirely.
// Before this fix, any request error at all -- a genuine dial-refused dead
// server, but just as much a 3s statusHealthProbeTimeout on a hung or
// overloaded server, or any other transport error -- collapsed into the same
// reachable == false. A hung server is not a dead one: it can still be
// actively relaying to (and trusting a token from) the very peer a revoke
// just tried to cut off. Only a clean connection refusal (the OS actively
// saying nothing is listening) is trustworthy evidence the process is
// actually down; treat everything else -- timeout included -- as reachable,
// just not healthy, since it never actually answered.
func probeStatusHealth(ctx context.Context, baseURL string) (reachable bool, healthy bool) {
	healthCtx, cancel := context.WithTimeout(ctx, statusHealthProbeTimeout)
	defer cancel()

	endpoint := strings.TrimRight(baseURL, "/") + "/healthz"
	request, err := http.NewRequestWithContext(healthCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return false, false
	}
	response, err := statusHealthHTTPClient.Do(request)
	if err != nil {
		return !isConnectionRefusedError(err), false
	}
	defer response.Body.Close()
	return true, response.StatusCode == http.StatusOK
}

// isConnectionRefusedError reports whether err's chain bottoms out at
// syscall.ECONNREFUSED: the OS actively refusing the connection because
// nothing is listening at all, as distinct from a context-deadline timeout
// or any other transport failure. http.Client wraps a dial failure as
// *url.Error -> *net.OpError -> *os.SyscallError -> syscall.Errno, and every
// layer in that chain implements Unwrap, so errors.Is walks all the way down
// to the errno without this needing to type-assert through it by hand.
func isConnectionRefusedError(err error) bool {
	return errors.Is(err, syscall.ECONNREFUSED)
}

// statusView is `moltnet status`'s printed JSON shape: every leg PLAN.md
// 7C.1 composes, each with its own *Error sibling so one leg failing (an
// agent token 403ing on /v1/pairings, say) degrades that one section instead
// of the whole command.
//
// ParticipantsTruncated/PeersTruncated: ListAgents/ListPairings are
// called with a zero PageRequest, so the server applies its own default page
// size (100, internal/transport/helpers.go's readLimit) rather than status
// paging to exhaustion -- an unbounded status call against a very large
// fleet would otherwise turn "who is in it" into a potentially expensive,
// slow-to-answer loop for a command whose job is a quick health snapshot.
// These two booleans surface PageInfo.HasMore instead, so "who is in it" is
// silently incomplete over 100 agents/peers no longer -- omitempty keeps
// them out of the common case where everything fit in one page.
type statusView struct {
	NetworkID             string                    `json:"network_id,omitempty"`
	BaseURL               string                    `json:"base_url"`
	Daemon                statusDaemonView          `json:"daemon"`
	Network               *protocol.Network         `json:"network,omitempty"`
	NetworkError          string                    `json:"network_error,omitempty"`
	Warnings              []protocol.NetworkWarning `json:"warnings,omitempty"`
	Participants          []protocol.AgentSummary   `json:"participants,omitempty"`
	ParticipantsError     string                    `json:"participants_error,omitempty"`
	ParticipantsTruncated bool                      `json:"participants_truncated,omitempty"`
	Peers                 []protocol.Pairing        `json:"peers,omitempty"`
	PeersError            string                    `json:"peers_error,omitempty"`
	PeersTruncated        bool                      `json:"peers_truncated,omitempty"`
	Metrics               *statusMetricsView        `json:"metrics,omitempty"`
	MetricsError          string                    `json:"metrics_error,omitempty"`
}

type statusDaemonView struct {
	Running bool   `json:"running"`
	Healthy bool   `json:"healthy"`
	Detail  string `json:"detail,omitempty"`
}

// statusMetricsView is --verbose's parsed summary of GET /metrics'
// Prometheus text exposition body (observability/metrics.go's
// RenderPrometheus): the handful of gauges/counters an operator actually
// wants at a glance, not the raw text -- see parseStatusMetrics.
type statusMetricsView struct {
	ActiveAttachments    int64 `json:"active_attachments"`
	ActiveSSESubscribers int64 `json:"active_sse_subscribers"`
	StoreHealthy         bool  `json:"store_healthy"`
	EventsDroppedTotal   int64 `json:"events_dropped_total"`
	HTTPRequestsTotal    int64 `json:"http_requests_total"`
}

// parseStatusMetrics extracts statusMetricsView's fields from raw, the
// verbatim Prometheus text exposition body observability.Metrics.ServeHTTP
// serves. It intentionally does not attempt a general Prometheus parser:
// every metric name RenderPrometheus emits is already known
// (internal/observability/metrics.go), so this only ever needs to recognize
// those exact names (or, for the one labeled counter this cares about,
// match a name prefix and sum every label combination) and ignore
// everything else, including comment lines and any future metric this view
// does not surface yet.
func parseStatusMetrics(raw string) statusMetricsView {
	var view statusMetricsView
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, value, ok := splitPrometheusMetricLine(line)
		if !ok {
			continue
		}
		switch {
		case name == "moltnet_active_attachments":
			view.ActiveAttachments = int64(value)
		case name == "moltnet_active_sse_subscribers":
			view.ActiveSSESubscribers = int64(value)
		case name == "moltnet_store_health":
			view.StoreHealthy = value == 1
		case name == "moltnet_events_dropped_total":
			view.EventsDroppedTotal = int64(value)
		case strings.HasPrefix(name, "moltnet_http_requests_total"):
			view.HTTPRequestsTotal += int64(value)
		}
	}
	return view
}

// splitPrometheusMetricLine splits one non-comment Prometheus text
// exposition line into its metric name (with any trailing {labels} left
// attached, so a caller matching by prefix -- moltnet_http_requests_total's
// per-route/method/status label set -- still sees them) and its trailing
// float value, exactly the "metric_name{labels} value" / "metric_name
// value" shape RenderPrometheus always emits (internal/observability/metrics.go).
func splitPrometheusMetricLine(line string) (name string, value float64, ok bool) {
	index := strings.LastIndex(line, " ")
	if index < 0 {
		return "", 0, false
	}
	parsed, err := strconv.ParseFloat(strings.TrimSpace(line[index+1:]), 64)
	if err != nil {
		return "", 0, false
	}
	return strings.TrimSpace(line[:index]), parsed, true
}

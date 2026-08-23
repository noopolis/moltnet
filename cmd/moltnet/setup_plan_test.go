package main

import (
	"strings"
	"testing"
)

func TestBuildSetupInitInvocationGlobal(t *testing.T) {
	answers := setupAnswers{
		scope:      setupScopeGlobal,
		id:         "acme",
		listenAddr: "127.0.0.1:8787",
		rooms:      []string{"general", "dev"},
	}
	invocation := buildSetupInitInvocation(answers)
	want := []string{"init", "--id", "acme", "--listen", "127.0.0.1:8787", "--room", "general", "--room", "dev"}
	if strings.Join(invocation.Args, " ") != strings.Join(want, " ") {
		t.Fatalf("buildSetupInitInvocation() = %v, want %v", invocation.Args, want)
	}
}

func TestBuildSetupInitInvocationProjectAddsDirFlag(t *testing.T) {
	answers := setupAnswers{
		scope:      setupScopeProject,
		id:         "local",
		listenAddr: "127.0.0.1:8787",
		rooms:      []string{"general"},
	}
	invocation := buildSetupInitInvocation(answers)
	want := []string{"init", "--dir", ".", "--id", "local", "--listen", "127.0.0.1:8787", "--room", "general"}
	if strings.Join(invocation.Args, " ") != strings.Join(want, " ") {
		t.Fatalf("buildSetupInitInvocation() = %v, want %v", invocation.Args, want)
	}
}

func TestSetupPlanInvocationsSkipsInitWhenAdopted(t *testing.T) {
	answers := setupAnswers{scope: setupScopeGlobal, id: "acme", service: true}
	invocations := setupPlanInvocations(answers, true)
	if len(invocations) != 1 || invocations[0].Args[0] != "service" {
		t.Fatalf("setupPlanInvocations(adopted=true) = %+v, want only the service install step", invocations)
	}
}

func TestSetupPlanInvocationsFreshIncludesInitFirst(t *testing.T) {
	answers := setupAnswers{scope: setupScopeGlobal, id: "acme", service: true, listenAddr: "127.0.0.1:8787", rooms: []string{"general"}}
	invocations := setupPlanInvocations(answers, false)
	if len(invocations) != 2 || invocations[0].Args[0] != "init" || invocations[1].Args[0] != "service" {
		t.Fatalf("setupPlanInvocations(adopted=false) = %+v, want init then service install", invocations)
	}
}

func TestSetupPlanInvocationsProjectNeverInstallsService(t *testing.T) {
	answers := setupAnswers{scope: setupScopeProject, id: "local", service: true, listenAddr: "127.0.0.1:8787", rooms: []string{"general"}}
	invocations := setupPlanInvocations(answers, false)
	if len(invocations) != 1 || invocations[0].Args[0] != "init" {
		t.Fatalf("setupPlanInvocations(project) = %+v, want only init even though service=true", invocations)
	}
}

func TestShellQuoteIfNeededLeavesPlainArgsAlone(t *testing.T) {
	if got := shellQuoteIfNeeded("acme-net"); got != "acme-net" {
		t.Fatalf("shellQuoteIfNeeded(%q) = %q", "acme-net", got)
	}
	if got := shellQuoteIfNeeded("127.0.0.1:8787"); got != "127.0.0.1:8787" {
		t.Fatalf("shellQuoteIfNeeded(%q) = %q", "127.0.0.1:8787", got)
	}
}

func TestShellQuoteIfNeededQuotesSpecialChars(t *testing.T) {
	got := shellQuoteIfNeeded("has space")
	if got != "'has space'" {
		t.Fatalf("shellQuoteIfNeeded(%q) = %q", "has space", got)
	}
	got = shellQuoteIfNeeded("it's")
	if got != `'it'\''s'` {
		t.Fatalf("shellQuoteIfNeeded(%q) = %q", "it's", got)
	}
}

// TestRenderChildInvocationRedactsEnvAsPlaceholder is the "secret-bearing
// env in printed commands" rule (SETUP.md's test plan): no invocation this
// unit builds actually carries Env yet (Q7's secret-bearing branches are
// the ones left "not yet implemented"), but the rendering rule itself must
// already hold for whenever they do.
func TestRenderChildInvocationRedactsEnvAsPlaceholder(t *testing.T) {
	invocation := childInvocation{
		Args: []string{"relay", "deploy", "--subdomain", "acme"},
		Env:  []string{"CLOUDFLARE_API_TOKEN=super-secret-value"},
	}
	rendered := renderChildInvocation(invocation)
	if strings.Contains(rendered, "super-secret-value") {
		t.Fatalf("renderChildInvocation() leaked the literal secret: %q", rendered)
	}
	if !strings.Contains(rendered, "CLOUDFLARE_API_TOKEN=<value from $CLOUDFLARE_API_TOKEN>") {
		t.Fatalf("renderChildInvocation() = %q, want a named placeholder for the env var", rendered)
	}
	if !strings.Contains(rendered, "moltnet relay deploy --subdomain acme") {
		t.Fatalf("renderChildInvocation() = %q, want the plain argv preserved", rendered)
	}
}

func TestStepLabelNamesKnownInvocations(t *testing.T) {
	if got := stepLabel(childInvocation{Args: []string{"init", "--id", "acme"}}); got != "network initialized" {
		t.Fatalf("stepLabel(init) = %q", got)
	}
	if got := stepLabel(childInvocation{Args: []string{"service", "install", "--id", "acme"}}); got != "service installed" {
		t.Fatalf("stepLabel(service install) = %q", got)
	}
	if got := stepLabel(childInvocation{Args: []string{"something", "else"}}); got != "something else" {
		t.Fatalf("stepLabel(unknown) = %q, want the raw argv joined", got)
	}
}

package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"
)

// promptLine prints question, reads one line from promptReader — the same
// echoed-answer seam promptYesNo (uninstall.go) and promptYesNoDefaultYes
// (relay_deploy_token.go) use — and returns it trimmed of surrounding
// whitespace. Unlike promptHidden (prompt_hidden.go), the answer is a
// normal, echoed terminal read: appropriate for a value that is not a
// secret, such as the workers.dev subdomain name the interactive claim
// prompt reads (relay_deploy_subdomain_claim.go).
//
// Unlike a plain read error (returned wrapped, as a non-nil error with an
// empty string), io.EOF (Ctrl-D, or closed/exhausted input) is returned
// as-is alongside whatever partial line was read before it — never
// swallowed into a nil error the way a bare "the operator hit Enter on an
// empty line" read would be — so a caller can tell the two apart: an
// empty-string-with-nil-error answer and an empty-string-with-io.EOF
// answer mean different things (relay_deploy_subdomain_claim.go's claim
// prompt treats EOF as an explicit decline, not an invalid empty name).
func promptLine(question string) (string, error) {
	fmt.Fprint(stdout, question)
	reader := bufio.NewReader(promptReader)
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("read input: %w", err)
	}
	return strings.TrimSpace(line), err
}

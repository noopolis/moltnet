package transport

import (
	"fmt"
	"html/template"
	"net/url"
	"strings"
)

func consoleAnalyticsSnippet(config ConsoleAnalyticsConfig) string {
	if strings.TrimSpace(config.Provider) != "google" {
		return ""
	}

	measurementID := strings.TrimSpace(config.MeasurementID)
	if measurementID == "" {
		return ""
	}

	sourceID := url.QueryEscape(measurementID)
	jsID := template.JSEscapeString(measurementID)

	return fmt.Sprintf(`<script async src="https://www.googletagmanager.com/gtag/js?id=%s"></script>
    <script>
      window.dataLayer = window.dataLayer || [];
      function gtag(){dataLayer.push(arguments);}
      gtag("js", new Date());
      gtag("config", "%s", { anonymize_ip: true });
    </script>`, sourceID, jsID)
}

func injectConsoleAnalytics(indexHTML string, config ConsoleAnalyticsConfig) string {
	snippet := consoleAnalyticsSnippet(config)
	if snippet == "" {
		return indexHTML
	}

	marker := "</head>"
	if !strings.Contains(indexHTML, marker) {
		return indexHTML + "\n" + snippet
	}

	return strings.Replace(indexHTML, marker, "    "+snippet+"\n  "+marker, 1)
}

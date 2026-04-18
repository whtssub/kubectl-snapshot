package snapshot

import "os"

const (
	ansiReset  = "\033[0m"
	ansiBold   = "\033[1m"
	ansiDim    = "\033[2m"
	ansiRed    = "\033[31m"
	ansiGreen  = "\033[32m"
	ansiYellow = "\033[33m"
)

func clr(code, s string) string {
	if os.Getenv("NO_COLOR") != "" {
		return s
	}
	return code + s + ansiReset
}

func colorizedSeverity(severity string) string {
	switch severity {
	case "HIGH":
		return clr(ansiRed, "🔴 HIGH")
	case "MEDIUM":
		return clr(ansiYellow, "🟡 MEDIUM")
	default:
		return clr(ansiGreen, "🟢 LOW")
	}
}

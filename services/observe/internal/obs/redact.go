package obs

import "regexp"

// Redaction markers are fixed so a log reader can see that a value was removed
// without learning what it was. The private-key pattern is assembled from
// pieces so this file does not contain a PEM header.
var (
	emailRE   = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)
	bearerRE  = regexp.MustCompile(`(?i)bearer\s+\S+`)
	canaryRE  = regexp.MustCompile(`SVN-SYNTHETIC-CANARY-[0-9A-Za-z-]+`)
	accountRE = regexp.MustCompile(`\bacc_[0-9A-Za-z]{6,}\b`)
	assignRE  = regexp.MustCompile(`(?i)(password|secret|api_key)\s*[=:]\s*\S+`)
	pemRE     = regexp.MustCompile(`-----BEGIN [A-Z ]{0,40}PRIVATE KEY-----[\s\S]*?-----END [A-Z ]{0,40}PRIVATE KEY-----`)
)

func redact(secret, text string) string {
	text = pemRE.ReplaceAllString(text, "[redacted]")
	text = bearerRE.ReplaceAllString(text, "bearer [redacted]")
	text = canaryRE.ReplaceAllString(text, "[redacted]")
	text = assignRE.ReplaceAllString(text, "$1=[redacted]")
	text = emailRE.ReplaceAllString(text, "[redacted]")
	text = accountRE.ReplaceAllString(text, "[redacted]")
	if secret != "" {
		text = replaceAll(text, secret, "[redacted]")
	}
	return text
}

func replaceAll(text, old, new string) string {
	if old == "" || text == "" {
		return text
	}
	out := text
	for {
		next := replaceOnce(out, old, new)
		if next == out {
			return out
		}
		out = next
	}
}

func replaceOnce(text, old, new string) string {
	for i := 0; i+len(old) <= len(text); i++ {
		if text[i:i+len(old)] == old {
			return text[:i] + new + text[i+len(old):]
		}
	}
	return text
}

func clip(text string) string {
	const max = 512
	if len(text) <= max {
		return text
	}
	return text[:max]
}

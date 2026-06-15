package ingest

import "regexp"

var (
	// AWS Access Key ID: 20-character uppercase alphanumeric string starting with AKIA or ASIA
	patternAWSAccessKey = regexp.MustCompile(`\b(AKIA|ASIA)[A-Z0-9]{16}\b`)
	// GitHub Personal Access Token (classic): starting with ghp_ followed by 36 alphanumeric characters
	patternGitHubToken  = regexp.MustCompile(`\bghp_[a-zA-Z0-9]{36}\b`)
	// Generic secret assignments: key = value or key: value
	patternSecretAssign = regexp.MustCompile("(?i)(\\b\\w*(?:password|passwd|secret|api_key|private_key|auth_token|token|passcode|passphrase|secret_key|access_token|session_key|session_secret)\\w*\\b\\s*[:=]\\s*)([^\\s\"'`]+|\"[^\"]*\"|'[^']*'|`[^`]*`)")
)

// RedactSecrets scans the line for AWS keys, GitHub tokens, or generic secret assignments
// and sanitizes them with [REDACTED_SECRET].
func RedactSecrets(line string) string {
	line = patternAWSAccessKey.ReplaceAllString(line, "[REDACTED_SECRET]")
	line = patternGitHubToken.ReplaceAllString(line, "[REDACTED_SECRET]")
	line = patternSecretAssign.ReplaceAllString(line, "${1}[REDACTED_SECRET]")
	return line
}

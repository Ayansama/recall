package ingest

import "regexp"

// Pre-compiled error identification patterns from the PRD error matrix.
var (
	patternGoPanic    = regexp.MustCompile(`^panic:`)
	patternPythonTB   = regexp.MustCompile(`^Traceback \(most recent call last\):`)
	patternNodeJS     = regexp.MustCompile(`^(Uncaught Exception:|UnhandledPromiseRejection:)`)
	patternGenericErr = regexp.MustCompile(`^(Error:|Fatal:|Exception:)`)

	errorPatterns = []*regexp.Regexp{
		patternGoPanic,
		patternPythonTB,
		patternNodeJS,
		patternGenericErr,
	}
)

// IsErrorLine reports whether cleansed text matches a known error signature.
func IsErrorLine(line string) bool {
	for _, re := range errorPatterns {
		if re.MatchString(line) {
			return true
		}
	}
	return false
}

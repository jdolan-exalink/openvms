package natsx

import (
	"strings"
	"testing"
)

func TestStreamsCaptureAlarmOpened(t *testing.T) {
	const subject = "alarm.opened.11111111-1111-1111-1111-111111111111"
	for _, s := range Streams {
		for _, pattern := range s.Subjects {
			if subjectMatches(pattern, subject) {
				return
			}
		}
	}
	t.Fatalf("no JetStream stream captures %s; publishes would fail with no stream", subject)
}

// subjectMatches implements NATS wildcard matching: "*" is one token, a trailing ">" the rest.
func subjectMatches(pattern, subject string) bool {
	p, s := strings.Split(pattern, "."), strings.Split(subject, ".")
	for i, tok := range p {
		if tok == ">" {
			return len(s) > i
		}
		if i >= len(s) || (tok != "*" && tok != s[i]) {
			return false
		}
	}
	return len(p) == len(s)
}

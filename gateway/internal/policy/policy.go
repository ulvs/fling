// Package policy decides what a user may share and for how long.
package policy

import (
	"fmt"
	"time"
)

type Policy struct {
	DefaultTTL     time.Duration
	MaxTTL         time.Duration
	MaxUploadBytes int64
}

// Violation is a rule the request broke, phrased for the person who sent it.
type Violation struct {
	Code    string
	Message string
}

func (v *Violation) Error() string { return v.Message }

func (p Policy) CheckUpload(size int64) error {
	if size <= 0 {
		return &Violation{Code: "empty_file", Message: "Empty files can't be shared."}
	}
	if size > p.MaxUploadBytes {
		return &Violation{
			Code:    "file_too_large",
			Message: fmt.Sprintf("Files over %s aren't allowed.", humanBytes(p.MaxUploadBytes)),
		}
	}
	return nil
}

// LinkTTL returns the lifetime for a new link: the default when none is requested,
// otherwise the request capped at the maximum.
func (p Policy) LinkTTL(requested time.Duration) time.Duration {
	if requested <= 0 {
		return p.DefaultTTL
	}
	return min(requested, p.MaxTTL)
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	value := float64(n) / float64(div)
	suffix := "KMGTPE"[exp]
	if value == float64(int64(value)) {
		return fmt.Sprintf("%d %cB", int64(value), suffix)
	}
	return fmt.Sprintf("%.1f %cB", value, suffix)
}

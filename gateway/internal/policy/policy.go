// Package policy decides what a user may share and for how long.
package policy

import (
	"fmt"
	"strings"
	"time"
	"unicode"
)

type Policy struct {
	DefaultTTL     time.Duration
	MaxTTL         time.Duration
	MaxUploadBytes int64
	// BlockedTypes are file endings that may not be shared (".exe", ".tar.gz").
	BlockedTypes []string
	// AllowedTypes, when set, are the only file endings that may be shared.
	AllowedTypes []string
}

// MaxSingleUpload is the largest file one presigned PUT can carry (S3's limit) until uploads
// in parts arrive.
const MaxSingleUpload = 5 << 30

// MaxLinkTTL bounds how long a link can be set to last.
const MaxLinkTTL = 365 * 24 * time.Hour

// ParseTypes turns ".EXE, bat,  .tar.gz" into [".exe", ".bat", ".tar.gz"].
func ParseTypes(raw string) []string {
	var out []string
	seen := map[string]bool{}
	for _, item := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ';' || unicode.IsSpace(r) }) {
		item = strings.ToLower(strings.TrimSpace(item))
		if item == "" || item == "." {
			continue
		}
		if !strings.HasPrefix(item, ".") {
			item = "." + item
		}
		if !seen[item] {
			seen[item] = true
			out = append(out, item)
		}
	}
	return out
}

func matchesType(name string, types []string) bool {
	name = strings.ToLower(name)
	for _, t := range types {
		if strings.HasSuffix(name, t) && len(name) > len(t) {
			return true
		}
	}
	return false
}

// CheckFile applies the size and file type rules to an upload.
func (p Policy) CheckFile(name string, size int64) error {
	if err := p.CheckUpload(size); err != nil {
		return err
	}
	if matchesType(name, p.BlockedTypes) {
		return &Violation{Code: "type_not_allowed", Message: "This type of file can't be shared here."}
	}
	if len(p.AllowedTypes) > 0 && !matchesType(name, p.AllowedTypes) {
		return &Violation{
			Code:    "type_not_allowed",
			Message: "Only these types of file can be shared here: " + strings.Join(p.AllowedTypes, ", ") + ".",
		}
	}
	return nil
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

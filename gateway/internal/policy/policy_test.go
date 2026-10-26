package policy

import (
	"errors"
	"strings"
	"testing"
	"time"
)

var testPolicy = Policy{DefaultTTL: 7 * 24 * time.Hour, MaxTTL: 30 * 24 * time.Hour, MaxUploadBytes: 2 << 30}

func TestCheckUpload(t *testing.T) {
	cases := []struct {
		size int64
		code string
	}{
		{1, ""},
		{2 << 30, ""},
		{2<<30 + 1, "file_too_large"},
		{0, "empty_file"},
		{-5, "empty_file"},
	}
	for _, c := range cases {
		err := testPolicy.CheckUpload(c.size)
		var v *Violation
		switch {
		case c.code == "" && err != nil:
			t.Errorf("size %d: unexpected error %v", c.size, err)
		case c.code != "" && (!errors.As(err, &v) || v.Code != c.code):
			t.Errorf("size %d: want %s, got %v", c.size, c.code, err)
		}
	}
}

func TestTooLargeMessageIsReadable(t *testing.T) {
	err := testPolicy.CheckUpload(3 << 30)
	if err == nil || err.Error() != "Files over 2 GB aren't allowed." {
		t.Fatalf("got %v", err)
	}
}

func TestLinkTTL(t *testing.T) {
	if got := testPolicy.LinkTTL(0); got != testPolicy.DefaultTTL {
		t.Errorf("no request: got %v", got)
	}
	if got := testPolicy.LinkTTL(time.Hour); got != time.Hour {
		t.Errorf("short request: got %v", got)
	}
	if got := testPolicy.LinkTTL(365 * 24 * time.Hour); got != testPolicy.MaxTTL {
		t.Errorf("long request should be capped: got %v", got)
	}
}

func TestParseTypes(t *testing.T) {
	got := ParseTypes(" .EXE, bat;  .tar.gz\n.exe  . ")
	want := []string{".exe", ".bat", ".tar.gz"}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestFileTypes(t *testing.T) {
	blocked := Policy{MaxUploadBytes: 100, BlockedTypes: ParseTypes(".exe, .tar.gz")}
	for name, ok := range map[string]bool{
		"setup.EXE":       false,
		"backup.tar.gz":   false,
		"notes.gz":        true,
		"report.pdf":      true,
		".exe":            true, // only an ending, no name: not the blocked type
		"exe":             true,
		"archive.exe.txt": true,
		"setup.exe.":      false, // Windows drops the trailing dot
		"setup.exe . ":    false,
	} {
		err := blocked.CheckFile(name, 10)
		if (err == nil) != ok {
			t.Errorf("blocked list, %q: %v", name, err)
		}
	}

	allowed := Policy{MaxUploadBytes: 100, AllowedTypes: ParseTypes("pdf, docx, zip")}
	for name, ok := range map[string]bool{
		"report.PDF": true,
		"folder.zip": true,
		"photo.jpg":  false,
		"README":     false,
	} {
		err := allowed.CheckFile(name, 10)
		if (err == nil) != ok {
			t.Errorf("allowed list, %q: %v", name, err)
		}
		var v *Violation
		if err != nil && (!errors.As(err, &v) || v.Code != "type_not_allowed" || !strings.Contains(v.Message, ".pdf, .docx, .zip")) {
			t.Errorf("%q: unexpected error %v", name, err)
		}
	}
	if err := allowed.CheckFile("big.pdf", 1000); err == nil {
		t.Error("size rule must still apply")
	}
}

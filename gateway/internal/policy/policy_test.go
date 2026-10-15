package policy

import (
	"errors"
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

package storage

import "testing"

func TestContentDisposition(t *testing.T) {
	cases := map[string]string{
		"report.pdf":         `attachment; filename="report.pdf"; filename*=UTF-8''report.pdf`,
		"räksmörgås.txt":     `attachment; filename="r_ksm_rg_s.txt"; filename*=UTF-8''r%C3%A4ksm%C3%B6rg%C3%A5s.txt`,
		`quote"and\back.txt`: `attachment; filename="quote_and_back.txt"; filename*=UTF-8''quote%22and%5Cback.txt`,
	}
	for in, want := range cases {
		if got := ContentDisposition(in); got != want {
			t.Errorf("%q:\n got %s\nwant %s", in, got, want)
		}
	}
}

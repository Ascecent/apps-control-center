package process

import (
	"os"
	"testing"
)

func TestParseStat(t *testing.T) {
	// After ) we need fields state ppid pgrp ... with rss at index 21
	rest := make([]string, 22)
	for i := range rest {
		rest[i] = "0"
	}
	rest[0] = "S"
	rest[1] = "1"
	rest[2] = "4242"
	rest[11] = "100"
	rest[12] = "50"
	rest[21] = "200"
	s := "999 (my proc) " + joinFields(rest)
	pgrp, ticks, rss, err := parseStat(s)
	if err != nil {
		t.Fatal(err)
	}
	if pgrp != 4242 {
		t.Fatalf("pgrp=%d", pgrp)
	}
	if ticks != 150 {
		t.Fatalf("ticks=%d", ticks)
	}
	wantRSS := 200 * uint64(os.Getpagesize())
	if rss != wantRSS {
		t.Fatalf("rss=%d want %d", rss, wantRSS)
	}
}

func joinFields(fs []string) string {
	out := fs[0]
	for i := 1; i < len(fs); i++ {
		out += " " + fs[i]
	}
	return out
}

func TestFormatUsage(t *testing.T) {
	got := FormatUsage(Usage{CPUPercent: 12.34, RSSBytes: 12 * 1024 * 1024, OK: true})
	if got != " 12.3%  12.0M" {
		t.Fatalf("got %q", got)
	}
	got = FormatUsage(Usage{})
	if got != "  —      —" {
		t.Fatalf("empty got %q", got)
	}
}

func TestSelfUsage(t *testing.T) {
	u1 := SelfUsage()
	if !u1.OK || u1.RSSBytes == 0 {
		t.Fatalf("first sample: %+v", u1)
	}
	// Burn a little CPU then sample again.
	n := 0
	for i := 0; i < 2_000_000; i++ {
		n += i
	}
	_ = n
	u2 := SelfUsage()
	if !u2.OK {
		t.Fatalf("second sample not ok")
	}
}

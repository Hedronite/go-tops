package acks_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Hedronite/go-tops/internal/acks"
)

func TestCommentParsesLawAndReason(t *testing.T) {
	law, reason, ok := acks.Comment("//tops:ack G-12 the ledger holds this debt")
	if !ok || law != "G-12" || reason != "the ledger holds this debt" {
		t.Fatalf("ok=%v law=%q reason=%q", ok, law, reason)
	}
}

func TestCommentRejectsShortReason(t *testing.T) {
	if _, _, ok := acks.Comment("//tops:ack G-12 two words"); ok {
		t.Fatal("want false for short reason")
	}
}

func TestCommentIgnoresNonAck(t *testing.T) {
	if _, _, ok := acks.Comment("//nolint:errcheck"); ok {
		t.Fatal("want false")
	}
}

func TestParseYAMLAcceptsValidEntry(t *testing.T) {
	doc := "acks:\n  - {law: G-12, file: a.go, reason: three words here, owner: x}\n"
	if _, err := acks.ParseYAML(".go-tops/acks.yaml", []byte(doc)); err != nil {
		t.Fatalf("err=%v", err)
	}
}

func TestParseYAMLRejectsBadLaw(t *testing.T) {
	doc := "acks:\n  - {law: BAD, file: a.go, reason: three words here, owner: x}\n"
	if _, err := acks.ParseYAML("acks.yaml", []byte(doc)); err == nil {
		t.Fatal("want error")
	}
}

func TestParseYAMLRejectsEmptyOwner(t *testing.T) {
	doc := "acks:\n  - {law: G-12, file: a.go, reason: three words here, owner: \"\"}\n"
	if _, err := acks.ParseYAML("acks.yaml", []byte(doc)); err == nil {
		t.Fatal("want error")
	}
}

func TestReasonOK(t *testing.T) {
	if !acks.ReasonOK("one two three") {
		t.Fatal("want true")
	}
	if acks.ReasonOK("one two") {
		t.Fatal("want false")
	}
}

func TestLoadFileRoundTrip(t *testing.T) {
	dir := t.TempDir()
	rel := ".go-tops/acks.yaml"
	body := "acks:\n  - {law: G-12, file: a.go, reason: three words here, owner: team}\n"
	if err := os.MkdirAll(filepath.Join(dir, ".go-tops"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, rel), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	doc, err := acks.LoadFile(dir, rel)
	if err != nil || len(doc.Acks) != 1 {
		t.Fatalf("doc=%+v err=%v", doc, err)
	}
	empty, err := acks.LoadFile(dir, "")
	if err != nil || len(empty.Acks) != 0 {
		t.Fatalf("empty rel=%+v err=%v", empty, err)
	}
}
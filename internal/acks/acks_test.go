package acks_test

import (
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
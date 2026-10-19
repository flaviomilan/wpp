package transcription

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

type cancellingModel struct {
	cancel context.CancelFunc
	reads  int
}

func (r *cancellingModel) Read(p []byte) (int, error) {
	r.reads++
	r.cancel()
	return copy(p, "partial model"), nil
}

func TestHashModelHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	model := &cancellingModel{cancel: cancel}
	if _, err := hashModel(ctx, model); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if model.reads != 1 {
		t.Fatal("read continued after cancellation", model.reads)
	}
	if _, err := hashModel(ctx, model); !errors.Is(err, context.Canceled) || model.reads != 1 {
		t.Fatal("cancelled read started", err)
	}
}

func TestHashModelPreservesDigest(t *testing.T) {
	text := strings.Repeat("modelo português ✅\n", 10000)
	digest := sha256.Sum256([]byte(text))
	actual, err := hashModel(context.Background(), strings.NewReader(text))
	if err != nil || actual != hex.EncodeToString(digest[:]) {
		t.Fatal(actual, err)
	}
}

package archive

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func BenchmarkExportJSONL(b *testing.B) {
	s, err := Open(b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	defer func() {
		if err := s.Close(); err != nil {
			b.Fatal(err)
		}
	}()
	if err = s.BindAccount("account@s.whatsapp.net"); err != nil {
		b.Fatal(err)
	}
	if err = s.Track("bench@g.us", true); err != nil {
		b.Fatal(err)
	}
	messages := make([]Message, 2000)
	text := strings.Repeat("Entrega prevista ✅\n", 160)
	for i := range messages {
		messages[i] = Message{Chat: "bench@g.us", ID: fmt.Sprint(i), Sender: "author@lid", Group: true, Kind: "texto", Text: text, Time: time.Unix(int64(i+1), 0)}
	}
	if _, err = s.Ingest(messages); err != nil {
		b.Fatal(err)
	}
	exporter := Exporter{Store: s, Dir: b.TempDir()}
	b.ReportAllocs()
	for b.Loop() {
		if err = exporter.ExportChatContext(context.Background(), "bench@g.us"); err != nil {
			b.Fatal(err)
		}
	}
}

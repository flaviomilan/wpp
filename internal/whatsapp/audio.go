package whatsapp

import (
	"context"
	"errors"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
	"io"
	"os"
	"wpp/internal/archive"
)

const maxAudioBytes = 64 * 1024 * 1024

func (c *Client) AudioReady() bool { return c.protocol().IsConnected() }

type limitedAudioFile struct{ *os.File }

func (f limitedAudioFile) Truncate(n int64) error {
	if n < 0 || n > maxAudioBytes+64 {
		return errors.New("áudio excede limite")
	}
	return f.File.Truncate(n)
}
func (f limitedAudioFile) WriteAt(p []byte, n int64) (int, error) {
	if n < 0 || n+int64(len(p)) > maxAudioBytes+64 {
		return 0, errors.New("áudio excede limite")
	}
	return f.File.WriteAt(p, n)
}
func (f limitedAudioFile) Write(p []byte) (int, error) {
	n, e := f.Seek(0, io.SeekCurrent)
	if e != nil {
		return 0, e
	}
	if n+int64(len(p)) > maxAudioBytes+64 {
		return 0, errors.New("áudio excede limite")
	}
	return f.File.Write(p)
}
func (c *Client) DownloadAudio(ctx context.Context, metadata []byte, path string) (result error) {
	var msg waE2E.AudioMessage
	if err := proto.Unmarshal(metadata, &msg); err != nil {
		return errors.New("metadados de áudio inválidos")
	}
	if msg.GetFileLength() > maxAudioBytes {
		return errors.New("áudio excede limite de download")
	}
	if !c.protocol().IsConnected() {
		return archive.ErrAudioOffline
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, file.Close()) }()
	if err = c.protocol().DownloadToFile(ctx, &msg, limitedAudioFile{file}); err != nil {
		if !c.protocol().IsConnected() {
			return archive.ErrAudioOffline
		}
		return errors.New("download de áudio indisponível")
	}
	return file.Sync()
}

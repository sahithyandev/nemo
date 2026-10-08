package technique

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"testing"

	"github.com/sahithyandev/nemo/internal/filesystem"
	"github.com/sahithyandev/nemo/internal/filesystem/fakefs"
)

func TestLegacySlackFrameReadAndClear(t *testing.T) {
	for _, data := range [][]byte{nil, []byte("legacy payload")} {
		frame := make([]byte, legacyFrameHeaderSize+len(data))
		copy(frame, legacyFrameMagic)
		binary.LittleEndian.PutUint32(frame[4:8], uint32(len(data)))
		binary.LittleEndian.PutUint32(frame[8:12], crc32.ChecksumIEEE(data))
		copy(frame[legacyFrameHeaderSize:], data)
		f := fakefs.New("/target")
		entry := f.Entry("/target")
		entry.Slack = []filesystem.SlackRegion{{Offset: 20, Length: int64(len(frame))}}
		copy(f.Img.Data[20:], frame)
		tech, _ := Get(SlackSpace)
		findings, err := tech.Detect(entry, Request{Image: f.Img})
		if err != nil || len(findings) != 1 {
			t.Fatalf("legacy detect: %v, %v", findings, err)
		}
		got, err := tech.Extract(entry, Request{Image: f.Img})
		if err != nil || !bytes.Equal(got, data) {
			t.Fatalf("legacy extract: %q, %v", got, err)
		}
		if _, err := tech.Clear(entry, Request{Image: f.Img}); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(f.Img.Data[20:20+len(frame)], make([]byte, len(frame))) {
			t.Fatal("legacy frame not cleared exactly")
		}
	}
}

func TestRepeatedPayloadStaleBackupRejected(t *testing.T) {
	f := fakefs.New("/target")
	entry := f.Entry("/target")
	entry.Slack = []filesystem.SlackRegion{{Offset: 20, Length: 100}}
	tech, _ := Get(SlackSpace)
	var first Backup
	if _, err := tech.Hide(entry, Request{Image: f.Img, Data: []byte("same"), Backup: func(b Backup) error { first = b; return nil }}); err != nil {
		t.Fatal(err)
	}
	if _, err := tech.Hide(entry, Request{Image: f.Img, Data: []byte("same")}); err != nil {
		t.Fatal(err)
	}
	before := append([]byte(nil), f.Img.Data...)
	if _, err := tech.Clear(entry, Request{Image: f.Img, Restore: first.Original, RestoreFrameSHA256: first.FrameSHA256}); err == nil {
		t.Fatal("stale identical-payload backup accepted")
	}
	if !bytes.Equal(before, f.Img.Data) {
		t.Fatal("failed restoration mutated image")
	}
}

func TestVersionedFrameNonceAndTruncation(t *testing.T) {
	a, err := encodeFrame(nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := encodeFrame(nil)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(a, b) {
		t.Fatal("repeated hide nonce collision")
	}
	for n := 0; n < len(a); n++ {
		if _, ok := decodeFrame(a[:n]); ok {
			t.Fatalf("accepted truncated v2 frame length %d", n)
		}
	}
	if data, ok := decodeFrame(a); !ok || len(data) != 0 {
		t.Fatal("empty v2 payload failed")
	}
}

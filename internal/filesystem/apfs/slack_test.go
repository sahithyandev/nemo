package apfs

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io/fs"
	"strings"
	"testing"

	"github.com/sahithyandev/nemo/internal/filesystem"
	"github.com/sahithyandev/nemo/internal/image"
	"github.com/sahithyandev/nemo/internal/technique"
)

// -------------------------------------------------------------------------
// inodeDStream: pure unit tests against hand-built xfield blobs.
// -------------------------------------------------------------------------

type xfieldEntry struct {
	typ  uint8
	data []byte
}

// buildXfieldsBlob assembles an xf_blob_t + x_field_t[] + values area from
// entries, padding each value up to the next 8-byte boundary the way a real
// APFS xfields blob does.
func buildXfieldsBlob(entries []xfieldEntry) []byte {
	descs := make([]byte, 4*len(entries))
	var values []byte
	for i, e := range entries {
		descs[i*4] = e.typ
		descs[i*4+1] = 0
		binary.LittleEndian.PutUint16(descs[i*4+2:i*4+4], uint16(len(e.data)))
		values = append(values, e.data...)
		if pad := (8 - len(e.data)%8) % 8; pad > 0 {
			values = append(values, make([]byte, pad)...)
		}
	}
	blob := make([]byte, 4)
	binary.LittleEndian.PutUint16(blob[0:2], uint16(len(entries)))
	binary.LittleEndian.PutUint16(blob[2:4], uint16(len(values)))
	blob = append(blob, descs...)
	blob = append(blob, values...)
	return blob
}

// buildInodeVal prepends a zeroed fixed j_inode_val_t part to xfields, so the
// result is a value inodeDStream can be called on directly.
func buildInodeVal(xfields []byte) []byte {
	val := make([]byte, inodeValFixedSize+len(xfields))
	copy(val[inodeValFixedSize:], xfields)
	return val
}

func encodeDStreamValue(size, allocedSize, cryptoID uint64) []byte {
	v := make([]byte, dstreamSize)
	binary.LittleEndian.PutUint64(v[0:8], size)
	binary.LittleEndian.PutUint64(v[8:16], allocedSize)
	binary.LittleEndian.PutUint64(v[16:24], cryptoID)
	return v
}

func TestInodeDStream(t *testing.T) {
	t.Run("no xfields at all", func(t *testing.T) {
		_, ok, err := inodeDStream(buildInodeVal(nil))
		if err != nil || ok {
			t.Fatalf("inodeDStream = (ok=%v, err=%v), want (false, nil)", ok, err)
		}
	})

	t.Run("dstream present, only xfield", func(t *testing.T) {
		xf := buildXfieldsBlob([]xfieldEntry{
			{typ: inoExtTypeDstream, data: encodeDStreamValue(4196, 8192, 0)},
		})
		ds, ok, err := inodeDStream(buildInodeVal(xf))
		if err != nil || !ok {
			t.Fatalf("inodeDStream = (ok=%v, err=%v), want (true, nil)", ok, err)
		}
		if ds.size != 4196 || ds.allocedSize != 8192 || ds.defaultCryptoID != 0 {
			t.Fatalf("ds = %+v, want size=4196 allocedSize=8192 cryptoID=0", ds)
		}
	})

	t.Run("dstream not first, with unaligned preceding value", func(t *testing.T) {
		xf := buildXfieldsBlob([]xfieldEntry{
			{typ: 4, data: []byte{1, 2, 3}}, // odd length forces padding
			{typ: inoExtTypeDstream, data: encodeDStreamValue(100, 100, 0)},
		})
		ds, ok, err := inodeDStream(buildInodeVal(xf))
		if err != nil || !ok {
			t.Fatalf("inodeDStream = (ok=%v, err=%v), want (true, nil)", ok, err)
		}
		if ds.size != 100 {
			t.Fatalf("ds.size = %d, want 100", ds.size)
		}
	})

	t.Run("no dstream xfield", func(t *testing.T) {
		xf := buildXfieldsBlob([]xfieldEntry{{typ: 4, data: []byte("name")}})
		_, ok, err := inodeDStream(buildInodeVal(xf))
		if err != nil || ok {
			t.Fatalf("inodeDStream = (ok=%v, err=%v), want (false, nil)", ok, err)
		}
	})

	t.Run("truncated blob shorter than xf_blob_t", func(t *testing.T) {
		val := buildInodeVal([]byte{1, 2})
		if _, _, err := inodeDStream(val); err == nil {
			t.Fatal("expected an error for a blob shorter than xf_blob_t")
		}
	})

	t.Run("num_exts overflows the blob", func(t *testing.T) {
		blob := make([]byte, 4)
		binary.LittleEndian.PutUint16(blob[0:2], 5) // claims 5 entries, none present
		if _, _, err := inodeDStream(buildInodeVal(blob)); err == nil {
			t.Fatal("expected an error for num_exts that doesn't fit the blob")
		}
	})

	t.Run("dstream xfield shorter than j_dstream_t", func(t *testing.T) {
		xf := buildXfieldsBlob([]xfieldEntry{{typ: inoExtTypeDstream, data: make([]byte, 10)}})
		if _, _, err := inodeDStream(buildInodeVal(xf)); err == nil {
			t.Fatal("expected an error for an undersized DSTREAM xfield")
		}
	})

	t.Run("inode record shorter than fixed part", func(t *testing.T) {
		if _, _, err := inodeDStream(make([]byte, inodeValFixedSize-1)); err == nil {
			t.Fatal("expected an error for a value shorter than j_inode_val_t")
		}
	})
}

// -------------------------------------------------------------------------
// slackFromExtents: pure unit tests against hand-built extent lists.
// -------------------------------------------------------------------------

func TestSlackFromExtents(t *testing.T) {
	const blockSize = 4096

	t.Run("single extent, partial tail", func(t *testing.T) {
		exts := []fileExtent{{logical: 0, length: 8192, phys: 10}}
		regions, err := slackFromExtents(exts, 4196, blockSize, 0, 1<<30)
		if err != nil {
			t.Fatalf("slackFromExtents: %v", err)
		}
		want := filesystem.SlackRegion{Offset: 10*blockSize + 4196, Length: 8192 - 4196}
		if len(regions) != 1 || regions[0] != want {
			t.Fatalf("regions = %+v, want [%+v]", regions, want)
		}
	})

	t.Run("size lands exactly on a block boundary: no slack", func(t *testing.T) {
		exts := []fileExtent{{logical: 0, length: 4096, phys: 5}}
		regions, err := slackFromExtents(exts, 4096, blockSize, 0, 1<<30)
		if err != nil {
			t.Fatalf("slackFromExtents: %v", err)
		}
		if regions != nil {
			t.Fatalf("regions = %+v, want nil", regions)
		}
	})

	t.Run("multi-extent: slack starts mid extent, next extent fully slack", func(t *testing.T) {
		exts := []fileExtent{
			{logical: 0, length: 4096, phys: 1},
			{logical: 4096, length: 4096, phys: 2},
			{logical: 8192, length: 4096, phys: 3},
		}
		size := uint64(4096 + 2000)
		regions, err := slackFromExtents(exts, size, blockSize, 0, 1<<30)
		if err != nil {
			t.Fatalf("slackFromExtents: %v", err)
		}
		want := []filesystem.SlackRegion{
			{Offset: 2*blockSize + 2000, Length: 4096 - 2000},
			{Offset: 3 * blockSize, Length: 4096},
		}
		if len(regions) != len(want) {
			t.Fatalf("regions = %+v, want %+v", regions, want)
		}
		for i := range want {
			if regions[i] != want[i] {
				t.Fatalf("regions[%d] = %+v, want %+v", i, regions[i], want[i])
			}
		}
	})

	t.Run("non-zero base is added to every offset", func(t *testing.T) {
		exts := []fileExtent{{logical: 0, length: 8192, phys: 10}}
		const base = int64(1) << 20
		regions, err := slackFromExtents(exts, 4196, blockSize, base, base+(1<<30))
		if err != nil {
			t.Fatalf("slackFromExtents: %v", err)
		}
		want := base + 10*blockSize + 4196
		if len(regions) != 1 || regions[0].Offset != want {
			t.Fatalf("regions = %+v, want offset %d", regions, want)
		}
	})

	t.Run("region past image end is rejected", func(t *testing.T) {
		exts := []fileExtent{{logical: 0, length: 4096, phys: 1000}}
		end := 1000*int64(blockSize) + 4096
		if _, err := slackFromExtents(exts, 0, blockSize, 0, end-1); err == nil {
			t.Fatal("expected an error for a region running past the image end")
		}
	})

	t.Run("no extents, no slack", func(t *testing.T) {
		regions, err := slackFromExtents(nil, 0, blockSize, 0, 1<<30)
		if err != nil || regions != nil {
			t.Fatalf("slackFromExtents(nil) = (%+v, %v), want (nil, nil)", regions, err)
		}
	})

	t.Run("a negative offset is skipped rather than reported", func(t *testing.T) {
		exts := []fileExtent{{logical: 0, length: 4096, phys: 0}}
		regions, err := slackFromExtents(exts, 0, blockSize, -8192, 1<<30)
		if err != nil {
			t.Fatalf("slackFromExtents: %v", err)
		}
		if regions != nil {
			t.Fatalf("regions = %+v, want nil (offset would be negative)", regions)
		}
	})
}

// -------------------------------------------------------------------------
// Entry.SlackRegions and the slack-space technique, against real fixtures.
// -------------------------------------------------------------------------

// fileContent reads path's logical content straight from its data-stream
// extents, bypassing SlackRegions entirely, so round-trip tests have an
// independent source of truth for "did the visible content change".
func fileContent(t *testing.T, f *FS, path string) []byte {
	t.Helper()
	e := entryFor(t, f, path)
	val, err := f.inodeRecordValue(e.oid)
	if err != nil {
		t.Fatalf("inodeRecordValue(%q): %v", path, err)
	}
	ds, ok, err := inodeDStream(val)
	if err != nil || !ok {
		t.Fatalf("inodeDStream(%q): ok=%v err=%v", path, ok, err)
	}
	privateID := binary.LittleEndian.Uint64(val[8:16])
	data, err := f.readExtents(privateID, ds.size)
	if err != nil {
		t.Fatalf("readExtents(%q): %v", path, err)
	}
	return data
}

// TestSlackRegionsRealFile checks the fixture's /slack.bin (4196 bytes,
// written by testdata/mkapfs.sh specifically to leave slack in its last
// block) produces exactly the expected, block-aligned region.
func TestSlackRegionsRealFile(t *testing.T) {
	f := openFS(t, loadImage(t, "apfs-gpt"))
	e := entryFor(t, f, "/slack.bin")

	regions, err := e.SlackRegions()
	if err != nil {
		t.Fatalf("SlackRegions: %v", err)
	}
	if len(regions) != 1 {
		t.Fatalf("len(regions) = %d, want 1: %+v", len(regions), regions)
	}
	r := regions[0]
	if r.Length != 3996 {
		t.Fatalf("Length = %d, want 3996", r.Length)
	}
	if r.Offset <= 0 {
		t.Fatalf("Offset = %d, want > 0 (apfs-gpt is GPT-wrapped)", r.Offset)
	}
	if (r.Offset+r.Length)%int64(f.blockSize) != 0 {
		t.Fatalf("region end %d is not aligned to block size %d", r.Offset+r.Length, f.blockSize)
	}
}

// TestSlackRoundTrip drives the slack-space technique end to end against a
// real file: hide must not disturb the logical content, detect must find the
// framed payload, and clear with the recorded backup must restore the exact
// original bytes.
func TestSlackRoundTrip(t *testing.T) {
	img := loadImage(t, "apfs-gpt")
	f := openFS(t, img)
	e := entryFor(t, f, "/slack.bin")
	original := fileContent(t, f, "/slack.bin")

	tech, err := technique.Get(technique.SlackSpace)
	if err != nil {
		t.Fatalf("technique.Get: %v", err)
	}

	var backups []technique.Backup
	payload := []byte("this is a hidden slack payload")
	result, err := tech.Hide(e, technique.Request{
		Data:  payload,
		Image: img,
		Backup: func(b technique.Backup) error {
			backups = append(backups, b)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("Hide: %v", err)
	}
	if result.Bytes != int64(len(payload)) {
		t.Fatalf("Hide result.Bytes = %d, want %d", result.Bytes, len(payload))
	}
	if len(backups) != 1 {
		t.Fatalf("len(backups) = %d, want 1", len(backups))
	}

	// Reopen from disk (re-verifies every touched node's checksum) and
	// confirm the file's logical content is untouched.
	f2 := openFS(t, img)
	after := fileContent(t, f2, "/slack.bin")
	if !bytes.Equal(after, original) {
		t.Fatalf("logical content changed after slack-space hide")
	}

	e2 := entryFor(t, f2, "/slack.bin")
	findings, err := tech.Detect(e2, technique.Request{Image: img})
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(findings) != 1 || findings[0].Size != int64(len(payload)) {
		t.Fatalf("Detect findings = %+v, want one finding of size %d", findings, len(payload))
	}

	clearResult, err := tech.Clear(e2, technique.Request{Image: img, Restore: backups[0].Original})
	if err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if !clearResult.Restored {
		t.Fatal("Clear result.Restored = false, want true (a backup was supplied)")
	}

	f3 := openFS(t, img)
	e3 := entryFor(t, f3, "/slack.bin")
	findings, err = tech.Detect(e3, technique.Request{Image: img})
	if err != nil {
		t.Fatalf("Detect after clear: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("findings after clear = %+v, want none", findings)
	}
	if !bytes.Equal(fileContent(t, f3, "/slack.bin"), original) {
		t.Fatalf("logical content changed after clear")
	}
}

// TestSlackClearZeroFillsWithoutRestore covers Clear when the caller has no
// recorded backup: the frame must be zeroed rather than left in place.
func TestSlackClearZeroFillsWithoutRestore(t *testing.T) {
	img := loadImage(t, "apfs-gpt")
	f := openFS(t, img)
	e := entryFor(t, f, "/slack.bin")
	tech, _ := technique.Get(technique.SlackSpace)

	if _, err := tech.Hide(e, technique.Request{Data: []byte("secret"), Image: img}); err != nil {
		t.Fatalf("Hide: %v", err)
	}

	f2 := openFS(t, img)
	e2 := entryFor(t, f2, "/slack.bin")
	result, err := tech.Clear(e2, technique.Request{Image: img})
	if err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if result.Restored {
		t.Fatal("Clear result.Restored = true, want false (no Restore bytes supplied)")
	}

	f3 := openFS(t, img)
	e3 := entryFor(t, f3, "/slack.bin")
	findings, err := tech.Detect(e3, technique.Request{Image: img})
	if err != nil {
		t.Fatalf("Detect after zero-fill clear: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("findings after clear = %+v, want none", findings)
	}
}

// TestSlackOversizedRejected confirms a payload too large for the available
// slack is rejected and leaves the file's content untouched.
func TestSlackOversizedRejected(t *testing.T) {
	img := loadImage(t, "apfs-gpt")
	f := openFS(t, img)
	e := entryFor(t, f, "/slack.bin")
	tech, _ := technique.Get(technique.SlackSpace)
	before := fileContent(t, f, "/slack.bin")

	// The only region is 3996 bytes; a 4096-byte payload plus the 12-byte
	// frame header cannot fit.
	_, err := tech.Hide(e, technique.Request{Data: make([]byte, 4096), Image: img})
	if err == nil || !strings.Contains(err.Error(), "insufficient slack space") {
		t.Fatalf("Hide oversized payload err = %v, want an 'insufficient slack space' error", err)
	}

	f2 := openFS(t, img)
	if !bytes.Equal(fileContent(t, f2, "/slack.bin"), before) {
		t.Fatal("file content changed despite a rejected oversized hide")
	}
}

// TestSlackDetectReadOnly confirms detect works through a read-only image
// wrapper (as cmd/detect.go always uses) while hide through the same
// wrapper is refused.
func TestSlackDetectReadOnly(t *testing.T) {
	img := loadImage(t, "apfs-gpt")
	f := openFS(t, img)
	e := entryFor(t, f, "/slack.bin")
	tech, _ := technique.Get(technique.SlackSpace)

	if _, err := tech.Hide(e, technique.Request{Data: []byte("secret"), Image: img}); err != nil {
		t.Fatalf("Hide: %v", err)
	}

	f2 := openFS(t, img)
	e2 := entryFor(t, f2, "/slack.bin")
	ro := image.ReadOnly(img)

	findings, err := tech.Detect(e2, technique.Request{Image: ro})
	if err != nil {
		t.Fatalf("Detect through read-only image: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("findings = %+v, want 1", findings)
	}

	if _, err := tech.Hide(e2, technique.Request{Data: []byte("more"), Image: ro}); !errors.Is(err, image.ErrReadOnly) {
		t.Fatalf("Hide through read-only image: err = %v, want image.ErrReadOnly", err)
	}
}

// TestSlackRegionsDirectory confirms a directory reports no slack and no
// error, rather than an error that would abort a whole-image scan.
func TestSlackRegionsDirectory(t *testing.T) {
	f := openFS(t, loadImage(t, "apfs-gpt"))
	root := f.Root().(*Entry)
	regions, err := root.SlackRegions()
	if err != nil {
		t.Fatalf("SlackRegions(root): %v", err)
	}
	if regions != nil {
		t.Fatalf("SlackRegions(root) = %+v, want nil", regions)
	}
}

// TestSlackRegionsEmptyFile confirms an empty file (no data stream at all,
// or a zero-size one) reports no slack and no error.
func TestSlackRegionsEmptyFile(t *testing.T) {
	f := openFS(t, loadImage(t, "apfs-manyfiles"))
	e := entryFor(t, f, "/f0000")
	regions, err := e.SlackRegions()
	if err != nil {
		t.Fatalf("SlackRegions: %v", err)
	}
	if regions != nil {
		t.Fatalf("SlackRegions(empty file) = %+v, want nil", regions)
	}
}

// TestSlackRegionsCompressedRefused inserts a filesystem-owned
// com.apple.decmpfs xattr (the same technique fixtures_test.go's
// TestSystemOwnedXattrRefused uses, since real HFS compression turns out not
// to be reliably producible with only builtin macOS tooling) and confirms
// SlackRegions refuses to report a region computed against a meaningless
// logical size.
func TestSlackRegionsCompressedRefused(t *testing.T) {
	f := openFS(t, loadImage(t, "apfs-gpt"))
	e := entryFor(t, f, "/slack.bin")

	val := encodeEmbeddedXattrVal([]byte("x"))
	binary.LittleEndian.PutUint16(val[0:2], xattrDataEmbedded|xattrFileSystemOwned)
	if err := f.insertXattr(e.oid, decmpfsXattrName, val); err != nil {
		t.Fatalf("insertXattr: %v", err)
	}

	_, err := e.SlackRegions()
	if err == nil {
		t.Fatal("SlackRegions: expected an error for a compressed file, got nil")
	}
	if !errors.Is(err, filesystem.ErrUnsupported) {
		t.Fatalf("SlackRegions err = %v, want it to wrap filesystem.ErrUnsupported", err)
	}
	if !strings.Contains(err.Error(), "compress") {
		t.Fatalf("SlackRegions err = %v, want it to mention compression", err)
	}
}

// dstreamCryptoIDOffset locates the byte offset of default_crypto_id within
// val's DSTREAM xfield, so a test can corrupt it in place without
// duplicating the whole xfields parser.
func dstreamCryptoIDOffset(t *testing.T, val []byte) int {
	t.Helper()
	xfields := val[inodeValFixedSize:]
	numExts := int(binary.LittleEndian.Uint16(xfields[0:2]))
	descOff := 4
	valOff := descOff + numExts*4
	for i := 0; i < numExts; i++ {
		entry := xfields[descOff+i*4 : descOff+i*4+4]
		typ := entry[0]
		size := int(binary.LittleEndian.Uint16(entry[2:4]))
		if typ == inoExtTypeDstream {
			return inodeValFixedSize + valOff + 16
		}
		valOff += (size + 7) &^ 7
	}
	t.Fatal("no DSTREAM xfield found on inode value")
	return 0
}

// TestSlackRegionsEncryptedStreamRefused patches /slack.bin's own DSTREAM
// xfield to claim a per-file crypto id this parser has no key for, and
// confirms SlackRegions refuses it the same way it refuses compression.
func TestSlackRegionsEncryptedStreamRefused(t *testing.T) {
	img := loadImage(t, "apfs-gpt")
	f := openFS(t, img)
	e := entryFor(t, f, "/slack.bin")

	val, err := f.inodeRecordValue(e.oid)
	if err != nil {
		t.Fatalf("inodeRecordValue: %v", err)
	}
	off := dstreamCryptoIDOffset(t, val)
	binary.LittleEndian.PutUint64(val[off:off+8], 99)

	err = f.fsTree.rewriteLeaf(encodeJKey(e.oid, objTypeInode), func(recs []record, _ bool) ([]record, int, error) {
		for i := range recs {
			if isInodeRecord(recs[i].key, e.oid) {
				recs[i].val = val
				return recs, 0, nil
			}
		}
		return nil, 0, fs.ErrNotExist
	})
	if err != nil {
		t.Fatalf("rewriteLeaf: %v", err)
	}

	f2 := openFS(t, img)
	e2 := entryFor(t, f2, "/slack.bin")
	_, err = e2.SlackRegions()
	if err == nil {
		t.Fatal("SlackRegions: expected an error for a per-file-encrypted stream, got nil")
	}
	if !errors.Is(err, filesystem.ErrUnsupported) {
		t.Fatalf("SlackRegions err = %v, want it to wrap filesystem.ErrUnsupported", err)
	}
	if !strings.Contains(err.Error(), "encrypt") {
		t.Fatalf("SlackRegions err = %v, want it to mention encryption", err)
	}
}

// TestSlackRegionsCorruptedDStreamXfieldErrors truncates /slack.bin's own
// xfields area to shorter than xf_blob_t and confirms SlackRegions surfaces
// a parse error rather than silently reporting no slack (which would look
// identical to a symlink or an empty file).
func TestSlackRegionsCorruptedDStreamXfieldErrors(t *testing.T) {
	img := loadImage(t, "apfs-gpt")
	f := openFS(t, img)
	e := entryFor(t, f, "/slack.bin")

	val, err := f.inodeRecordValue(e.oid)
	if err != nil {
		t.Fatalf("inodeRecordValue: %v", err)
	}
	corrupted := append([]byte(nil), val[:inodeValFixedSize+2]...)

	err = f.fsTree.rewriteLeaf(encodeJKey(e.oid, objTypeInode), func(recs []record, _ bool) ([]record, int, error) {
		for i := range recs {
			if isInodeRecord(recs[i].key, e.oid) {
				recs[i].val = corrupted
				return recs, 0, nil
			}
		}
		return nil, 0, fs.ErrNotExist
	})
	if err != nil {
		t.Fatalf("rewriteLeaf: %v", err)
	}

	f2 := openFS(t, img)
	e2 := entryFor(t, f2, "/slack.bin")
	if _, err := e2.SlackRegions(); err == nil {
		t.Fatal("SlackRegions: expected an error for a truncated xfields blob, got nil")
	} else if errors.Is(err, filesystem.ErrUnsupported) {
		t.Fatalf("SlackRegions err = %v, want a parse error, not ErrUnsupported", err)
	}
}

// TestSlackRegionsMissingExtentsErrors points /slack.bin's private_id at an
// object id with no FILE_EXTENT records and confirms SlackRegions surfaces
// that as an error instead of silently reporting no slack.
func TestSlackRegionsMissingExtentsErrors(t *testing.T) {
	img := loadImage(t, "apfs-gpt")
	f := openFS(t, img)
	e := entryFor(t, f, "/slack.bin")

	val, err := f.inodeRecordValue(e.oid)
	if err != nil {
		t.Fatalf("inodeRecordValue: %v", err)
	}
	corrupted := append([]byte(nil), val...)
	binary.LittleEndian.PutUint64(corrupted[8:16], 0xDEADBEEF) // private_id

	err = f.fsTree.rewriteLeaf(encodeJKey(e.oid, objTypeInode), func(recs []record, _ bool) ([]record, int, error) {
		for i := range recs {
			if isInodeRecord(recs[i].key, e.oid) {
				recs[i].val = corrupted
				return recs, 0, nil
			}
		}
		return nil, 0, fs.ErrNotExist
	})
	if err != nil {
		t.Fatalf("rewriteLeaf: %v", err)
	}

	f2 := openFS(t, img)
	e2 := entryFor(t, f2, "/slack.bin")
	_, err = e2.SlackRegions()
	if err == nil {
		t.Fatal("SlackRegions: expected an error for a private_id with no extents, got nil")
	}
	if !strings.Contains(err.Error(), "extent") {
		t.Fatalf("SlackRegions err = %v, want it to mention extents", err)
	}
}

// TestDetectorAdvertisesSlack confirms the registered APFS detector lists
// slack-space among its supported techniques.
func TestDetectorAdvertisesSlack(t *testing.T) {
	for _, d := range filesystem.Detectors() {
		if d.Type != filesystem.TypeAPFS {
			continue
		}
		for _, name := range d.Techniques {
			if name == "slack-space" {
				return
			}
		}
		t.Fatalf("apfs detector Techniques = %v, want it to include slack-space", d.Techniques)
	}
	t.Fatal("no apfs detector registered")
}

package ext4

import (
	"testing"

	"github.com/sahithyandev/nemo/internal/filesystem"
)

func TestSlackRegionsUsesTailOfFinalMappedBlock(t *testing.T) {
	img := syntheticImage()
	setFileExtents(img, 3, 100, []testExtent{{logical: 0, length: 1, physical: 20}})
	entry := slackTestEntry(t, img)

	regions, err := entry.SlackRegions()
	if err != nil {
		t.Fatal(err)
	}
	want := filesystem.SlackRegion{Offset: 20*testBlockSize + 100, Length: testBlockSize - 100}
	if len(regions) != 1 || regions[0] != want {
		t.Fatalf("SlackRegions = %+v; want [%+v]", regions, want)
	}
}

func TestSlackRegionsSupportsMultipleExtents(t *testing.T) {
	img := syntheticImage()
	setFileExtents(img, 3, testBlockSize+73, []testExtent{
		{logical: 0, length: 1, physical: 20},
		{logical: 1, length: 1, physical: 22},
	})
	entry := slackTestEntry(t, img)

	regions, err := entry.SlackRegions()
	if err != nil {
		t.Fatal(err)
	}
	want := filesystem.SlackRegion{Offset: 22*testBlockSize + 73, Length: testBlockSize - 73}
	if len(regions) != 1 || regions[0] != want {
		t.Fatalf("SlackRegions = %+v; want [%+v]", regions, want)
	}
}

func TestSlackRegionsReturnsNoneForBlockAlignedFile(t *testing.T) {
	img := syntheticImage()
	setFileExtents(img, 3, testBlockSize, []testExtent{{logical: 0, length: 1, physical: 20}})
	regions, err := slackTestEntry(t, img).SlackRegions()
	if err != nil {
		t.Fatal(err)
	}
	if len(regions) != 0 {
		t.Fatalf("SlackRegions = %+v; want none", regions)
	}
}

type testExtent struct {
	logical  uint32
	length   uint16
	physical uint32
}

func setFileExtents(img *testImage, number uint32, size uint32, extents []testExtent) {
	off := 5*testBlockSize + int(number-1)*256
	in := img.data[off : off+256]
	put16(in, 0, modeRegular)
	put32(in, 4, size)
	put32(in, 32, inodeFlagExtents)
	put16(in, 40, extentMagic)
	put16(in, 42, uint16(len(extents)))
	put16(in, 44, 4)
	put16(in, 46, 0)
	for i, extent := range extents {
		record := 52 + i*12
		put32(in, record, extent.logical)
		put16(in, record+4, extent.length)
		put32(in, record+8, extent.physical)
	}
}

func slackTestEntry(t *testing.T, img *testImage) *Entry {
	t.Helper()
	fsi, err := New(img)
	if err != nil {
		t.Fatal(err)
	}
	entry, err := fsi.Open("/hello.txt")
	if err != nil {
		t.Fatal(err)
	}
	return entry.(*Entry)
}

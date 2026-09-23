package cmd

import (
	"runtime"

	"github.com/sahithyandev/nemo/internal/custody"
	"github.com/sahithyandev/nemo/internal/filesystem/apfs"
	"github.com/sahithyandev/nemo/internal/filesystem/ntfs"
	imagepkg "github.com/sahithyandev/nemo/internal/image"
	"github.com/sahithyandev/nemo/internal/technique"
)

// openLiveTarget uses read-only NTFS volume access on Windows. On macOS,
// named-stream and
// timestomp go through a path-backed Entry (xattr and setattrlist
// syscalls, no special privilege needed). Slack-space is volume-level and
// needs the raw device backing target's mounted volume, which is only
// attempted when tech explicitly asks for it. An unqualified detect scan
// should not reach for a device it may not have permission to open; see
// docs/architecture/live-mode.md.
func openLiveTarget(target, tech string, write bool) (openedTarget, error) {
	if runtime.GOOS == "windows" {
		fs, img, closeFn, err := ntfs.OpenLive(target, write, liveImageWrap(write))
		if err != nil {
			return openedTarget{}, err
		}
		return openedTarget{filesystem: fs, image: img, close: closeFn}, nil
	}
	if tech == technique.SlackSpace {
		return openLiveSlack(target, write)
	}
	fs, err := apfs.OpenLive(target)
	if err != nil {
		return openedTarget{}, err
	}
	return openedTarget{filesystem: fs}, nil
}

// openLiveSlack opens the device backing target's volume (buffered for a
// write, raw character device for a read; see apfs.OpenLiveSlack). The wrap
// closure applies the same custody-logging policy image mode uses for a
// write, or a plain read-only wrapper for a scan, so a live slack-space
// mutation goes through custody logging exactly like an image-mode one:
// it is never handed the unwrapped device.
func openLiveSlack(target string, write bool) (openedTarget, error) {
	fs, img, closeFn, err := apfs.OpenLiveSlack(target, write, liveImageWrap(write))
	if err != nil {
		return openedTarget{}, err
	}
	return openedTarget{filesystem: fs, image: img, close: closeFn}, nil
}

func liveImageWrap(write bool) ntfs.LiveWrap {
	return func(raw imagepkg.Image, rawClose func() error) (imagepkg.Image, func() error) {
		if !write {
			return imagepkg.ReadOnly(raw), rawClose
		}
		recorder := custody.Wrap(raw)
		return recorder, recorder.Close
	}
}

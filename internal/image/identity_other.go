//go:build !unix && !windows

package image

import "fmt"

func (r *RawImage) BackupIdentity() (string, error) {
	return "", fmt.Errorf("durable backing-file identity unsupported on this platform")
}

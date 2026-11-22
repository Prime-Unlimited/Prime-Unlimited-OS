package usbcopy

import (
	"errors"
	"golang.org/x/sys/unix"
	"math"
)

// Reserve for the offline database backup, rollback journal, artwork and five
// percent of currently free space, not just the audio payload.
func batchWorkingBytes(decisions []DuplicateDecision, databaseBytes int64) (int64, error) {
	if databaseBytes < 0 || databaseBytes > math.MaxInt64/2 {
		return 0, errors.New("invalid database size")
	}
	needed := databaseBytes * 2
	add := func(bytes int64) error {
		if bytes < 0 || needed > math.MaxInt64-bytes {
			return errors.New("import working size overflow")
		}
		needed += bytes
		return nil
	}
	if e := add(16 << 20); e != nil {
		return 0, e
	}
	for _, d := range decisions {
		if d.ExistingTrackID == 0 && d.DuplicateOfSource == "" {
			if e := add(d.Fingerprint.Bytes); e != nil {
				return 0, e
			}
			if e := add(256 << 10); e != nil {
				return 0, e
			}
		}
	}
	return needed, nil
}
func requireBatchSpace(root string, needed int64) error {
	var s unix.Statfs_t
	if e := unix.Statfs(root, &s); e != nil {
		return e
	}
	if needed < 0 || s.Bsize <= 0 {
		return errors.New("invalid available-space measurement")
	}
	blocks, size := uint64(s.Bavail), uint64(s.Bsize)
	if blocks > math.MaxUint64/size {
		return errors.New("available space overflow")
	}
	free := blocks * size
	if uint64(needed) > free-free/20 {
		return errors.New("destination lacks safe working space")
	}
	return nil
}

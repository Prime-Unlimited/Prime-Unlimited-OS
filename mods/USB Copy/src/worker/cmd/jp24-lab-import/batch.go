package main

import (
	"context"
	"fmt"
	usbcopy "prime-unlimited.usb-copy/worker"
)

func runBatch(sourceVolume, destinationVolume, procRoot string) error {
	result, err := usbcopy.RunOfflineBatch(context.Background(), sourceVolume, destinationVolume, procRoot, usbcopy.OfflineBatchOptions{})
	if err != nil {
		return err
	}
	fmt.Printf("BATCH_COMMITTED; copied=%d reused=%d batch_duplicates=%d playlists=%d\n", result.Copied, result.Reused, result.SourceDuplicates, result.Playlists)
	fmt.Println("PLAYLIST: " + result.PlaylistName)
	return nil
}

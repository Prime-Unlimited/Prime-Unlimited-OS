package usbcopy

import "errors"

// BuildImportPlan uses destination paths for filesystem playlists. The live
// sequence uses source identities until native registration has verified IDs.
func LivePlanWithSourcePlaylists(plan ImportPlan) (ImportPlan, error) {
	out := plan
	byDestination := map[string]string{}
	for _, t := range plan.Tracks {
		if t.SourceRelative == "" || t.DestinationRelative == "" || byDestination[t.DestinationRelative] != "" {
			return out, errors.New("ambiguous live plan destination")
		}
		byDestination[t.DestinationRelative] = t.SourceRelative
	}
	convert := func(p PlannedPlaylist) (PlannedPlaylist, error) {
		paths := make([]string, 0, len(p.Tracks))
		for _, dest := range p.Tracks {
			source, ok := byDestination[dest]
			if !ok {
				return p, errors.New("unknown live playlist destination")
			}
			paths = append(paths, source)
		}
		p.Tracks = paths
		return p, nil
	}
	var err error
	out.CopyPlaylist, err = convert(plan.CopyPlaylist)
	if err != nil {
		return out, err
	}
	out.SourcePlaylists = make([]PlannedPlaylist, len(plan.SourcePlaylists))
	for i, p := range plan.SourcePlaylists {
		out.SourcePlaylists[i], err = convert(p)
		if err != nil {
			return out, err
		}
	}
	return out, nil
}

// The live snapshot is stored on the same internal drive as the copied audio.
func RequireLiveImportSpace(root string, decisions []DuplicateDecision, databaseBytes int64) error {
	needed, err := batchWorkingBytes(decisions, databaseBytes)
	if err != nil {
		return err
	}
	return requireBatchSpace(root, needed)
}

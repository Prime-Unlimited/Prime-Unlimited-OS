package usbcopy

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// LiveBinding is obtained by the trusted native broker from the actual selected
// internal library and running Engine instance, never from package QML.
type LiveBinding struct{ LibraryUUID, MountIdentity, EngineIdentity string }
type LiveTrackReference struct {
	SourceRelative, NativeID string
	LocalID                  int64
	Binding                  LiveBinding
}
type LivePlaylistReference struct {
	NativeID string
	LocalID  int64
	Binding  LiveBinding
}
type LiveSequenceState struct {
	SourceDigest string                  `json:"sourceDigest"`
	JobID        string                  `json:"jobId"`
	Phase        string                  `json:"phase"`
	Intent       string                  `json:"intent,omitempty"`
	Binding      LiveBinding             `json:"binding"`
	Skipped      int                     `json:"skipped"`
	Tracks       []LiveTrackReference    `json:"tracks,omitempty"`
	Playlists    []LivePlaylistReference `json:"playlists,omitempty"`
}

// Create must exclusively reserve JobID, including across process restarts.
// Save must atomically persist and sync before returning. Neither method may
// silently resume/overwrite an existing sequence. A failed Save stops all calls.
type LiveSequenceJournal interface {
	Create(context.Context, LiveSequenceState) error
	Save(context.Context, LiveSequenceState) error
}

// The adapter must validate internal mount/library identity and observe IDs from
// the current native model. Register verifies metadata/artwork and registration;
// Add verifies membership. Copy verifies hashes against approved fingerprints.
// There is deliberately no Engine stop, SQL-write or automatic retry operation.
type LiveSequenceAdapter interface {
	Validate(context.Context) (LiveBinding, error)
	Backup(context.Context, string) error
	Copy(context.Context, string, ImportPlan, []DuplicateDecision) error
	Register(context.Context, string, PlannedTrack) (LiveTrackReference, error)
	CreatePlaylist(context.Context, string, string) (LivePlaylistReference, error)
	Add(context.Context, string, LivePlaylistReference, []LiveTrackReference) error
	Verify(context.Context, ImportPlan, LiveSequenceState) error
}

// LiveNewOnlyPlan rejects incomplete/reordered/stale duplicate decisions and
// filters BOTH generated/source playlists to only files actually copied. Aliases
// of newly copied files are skipped as requested, not added to the playlists.
func LiveNewOnlyPlan(plan ImportPlan, decisions []DuplicateDecision) (ImportPlan, int, error) {
	out := plan
	out.Tracks = nil
	out.SourcePlaylists = nil
	out.BytesToCopy = 0
	out.CopyPlaylist = plan.CopyPlaylist
	out.CopyPlaylist.Tracks = nil
	if len(plan.Tracks) != len(decisions) {
		return out, 0, errors.New("incomplete live duplicate decisions")
	}
	allowed := map[string]bool{}
	seen := map[string]bool{}
	fingerprints := map[string]FileFingerprint{}
	skipped := 0
	for i, t := range plan.Tracks {
		d := decisions[i]
		if d.SourceRelative != t.SourceRelative || seen[t.SourceRelative] || d.Fingerprint.Bytes != t.Bytes || d.Fingerprint.ModifiedAt != t.SourceModifiedAt || len(d.Fingerprint.SHA256) != 64 {
			return out, 0, errors.New("source changed after live preview")
		}
		for _, c := range d.Fingerprint.SHA256 {
			if !strings.ContainsRune("0123456789abcdef", c) {
				return out, 0, errors.New("invalid fingerprint")
			}
		}
		seen[t.SourceRelative] = true
		fingerprints[t.SourceRelative] = d.Fingerprint
		if d.ExistingTrackID < 0 || (d.ExistingTrackID > 0 && d.DuplicateOfSource != "") {
			return out, 0, errors.New("conflicting duplicate decision")
		}
		if d.DuplicateOfSource != "" && (d.DuplicateOfSource == d.SourceRelative || !seen[d.DuplicateOfSource] || fingerprints[d.DuplicateOfSource].SHA256 != d.Fingerprint.SHA256 || fingerprints[d.DuplicateOfSource].Bytes != d.Fingerprint.Bytes) {
			return out, 0, errors.New("unresolved duplicate source")
		}
		if d.ExistingTrackID > 0 || d.DuplicateOfSource != "" {
			skipped++
			continue
		}
		if t.Bytes <= 0 || t.Bytes > int64(^uint64(0)>>1)-out.BytesToCopy {
			return out, 0, errors.New("invalid live copy size")
		}
		allowed[t.SourceRelative] = true
		out.Tracks = append(out.Tracks, t)
		out.BytesToCopy += t.Bytes
	}
	filter := func(paths []string) ([]string, error) {
		result := []string{}
		added := map[string]bool{}
		for _, p := range paths {
			if !seen[p] {
				return nil, errors.New("playlist references unknown source")
			}
			if allowed[p] && !added[p] {
				result = append(result, p)
				added[p] = true
			}
		}
		return result, nil
	}
	var err error
	out.CopyPlaylist.Tracks, err = filter(plan.CopyPlaylist.Tracks)
	if err != nil {
		return out, 0, err
	}
	if len(out.CopyPlaylist.Tracks) != len(out.Tracks) {
		return out, 0, errors.New("generated playlist does not cover copied files")
	}
	for _, p := range plan.SourcePlaylists {
		p.Tracks, err = filter(p.Tracks)
		if err != nil {
			return out, 0, err
		}
		if len(p.Tracks) > 0 {
			out.SourcePlaylists = append(out.SourcePlaylists, p)
		}
	}
	return out, skipped, nil
}
func validLiveNativeTrackID(id string) bool {
	n, err := strconv.ParseUint(id, 10, 64)
	return err == nil && n > 0 && n <= (1<<53)-1
}
func validLiveBinding(b LiveBinding) bool {
	return b.LibraryUUID != "" && b.MountIdentity != "" && b.EngineIdentity != ""
}

// RunLiveSequence is the reusable orchestration core. It is not wired into HTTP
// admission yet: a concrete durable native broker and shared writer lease must
// be supplied by the trusted runtime. Existing offline import stays unchanged.
func RunLiveSequence(ctx context.Context, id string, plan ImportPlan, decisions []DuplicateDecision, journal LiveSequenceJournal, adapter LiveSequenceAdapter) (LiveSequenceState, error) {
	state := LiveSequenceState{JobID: id, Phase: "prepared"}
	if !validJobStatusID(id) || journal == nil || adapter == nil {
		return state, errors.New("invalid live sequence configuration")
	}
	filtered, skipped, err := LiveNewOnlyPlan(plan, decisions)
	if err != nil {
		return state, err
	}
	state.Skipped = skipped
	state.SourceDigest = DuplicateSourceDigest(decisions)
	copiedDecisions := []DuplicateDecision{}
	for _, d := range decisions {
		if d.ExistingTrackID == 0 && d.DuplicateOfSource == "" {
			copiedDecisions = append(copiedDecisions, d)
		}
	}
	playlists := append([]PlannedPlaylist{filtered.CopyPlaylist}, filtered.SourcePlaylists...)
	names := map[string]bool{}
	if len(filtered.Tracks) > 0 {
		for _, p := range playlists {
			if strings.TrimSpace(p.Name) == "" || names[p.Name] {
				return state, errors.New("ambiguous live playlist name")
			}
			names[p.Name] = true
		}
	}
	state.Binding, err = adapter.Validate(ctx)
	if err != nil || !validLiveBinding(state.Binding) {
		return state, errors.New("live internal library binding unavailable")
	}
	if err = journal.Create(ctx, state); err != nil {
		return state, err
	}
	// Persist intent BEFORE each side effect; an ambiguous return is never retried.
	effect := func(intent string, call func() error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		binding, err := adapter.Validate(ctx)
		if err != nil || binding != state.Binding {
			return errors.New("live library or Engine changed")
		}
		state.Phase = "pending"
		state.Intent = id + ":" + intent
		if err = journal.Save(ctx, state); err != nil {
			return err
		}
		if err = call(); err != nil {
			return err
		}
		state.Phase = "confirmed"
		state.Intent = ""
		return journal.Save(ctx, state)
	}
	finishFailure := func(err error) (LiveSequenceState, error) {
		state.Phase = "review_required"
		reviewCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		saveErr := journal.Save(reviewCtx, state)
		return state, errors.Join(err, saveErr)
	}
	if len(filtered.Tracks) == 0 {
		state.Phase = "completed"
		return state, journal.Save(ctx, state)
	}
	if err = effect("backup", func() error { return adapter.Backup(ctx, id) }); err != nil {
		return finishFailure(err)
	}
	if err = effect("copy", func() error { return adapter.Copy(ctx, id, filtered, copiedDecisions) }); err != nil {
		return finishFailure(err)
	}
	bySource := map[string]LiveTrackReference{}
	nativeIDs := map[string]bool{}
	localIDs := map[int64]bool{}
	for i, t := range filtered.Tracks {
		err = effect(fmt.Sprintf("register:%d", i), func() error {
			ref, e := adapter.Register(ctx, state.Intent, t)
			if e != nil {
				return e
			}
			if ref.Binding != state.Binding || ref.SourceRelative != t.SourceRelative || !validLiveNativeTrackID(ref.NativeID) || ref.LocalID <= 0 || nativeIDs[ref.NativeID] || localIDs[ref.LocalID] {
				return errors.New("unverified or duplicate native track identity")
			}
			nativeIDs[ref.NativeID] = true
			localIDs[ref.LocalID] = true
			bySource[t.SourceRelative] = ref
			state.Tracks = append(state.Tracks, ref)
			return nil
		})
		if err != nil {
			return finishFailure(err)
		}
	}
	listIDs := map[string]bool{}
	for i, p := range playlists {
		var list LivePlaylistReference
		err = effect(fmt.Sprintf("create:%d", i), func() error {
			var e error
			list, e = adapter.CreatePlaylist(ctx, state.Intent, p.Name)
			if e != nil {
				return e
			}
			if list.Binding != state.Binding || list.NativeID == "" || list.LocalID <= 0 || listIDs[list.NativeID] {
				return errors.New("unverified or repeated native playlist identity")
			}
			listIDs[list.NativeID] = true
			state.Playlists = append(state.Playlists, list)
			return nil
		})
		if err != nil {
			return finishFailure(err)
		}
		refs := make([]LiveTrackReference, 0, len(p.Tracks))
		for _, source := range p.Tracks {
			refs = append(refs, bySource[source])
		}
		if err = effect(fmt.Sprintf("add:%d", i), func() error { return adapter.Add(ctx, state.Intent, list, refs) }); err != nil {
			return finishFailure(err)
		}
	}
	if err = effect("verify", func() error { return adapter.Verify(ctx, filtered, state) }); err != nil {
		return finishFailure(err)
	}
	state.Phase = "completed"
	return state, journal.Save(ctx, state)
}

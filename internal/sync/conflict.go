package sync

import (
	"context"
	"errors"
	"fmt"

	"github.com/jookos/obgo/internal/couchdb"
)

// resolveConflicts resolves CouchDB revision conflicts on a meta document.
// It fetches all conflicting revisions, picks the one with the highest MTime,
// rewrites it as the current revision if needed, and tombstones all losing branches.
// Returns the authoritative MetaDoc to apply to disk.
func (s *Service) resolveConflicts(ctx context.Context, doc couchdb.MetaDoc) (couchdb.MetaDoc, error) {
	if len(doc.Conflicts) == 0 {
		return doc, nil
	}

	winner := doc
	var losers []string

	for _, rev := range doc.Conflicts {
		candidate, err := s.db.GetMetaAtRev(ctx, doc.ID, rev)
		if err != nil {
			if errors.Is(err, couchdb.ErrNotFound) {
				// Already tombstoned; nothing to do for this branch.
				continue
			}
			// Unexpected error; skip but still try to delete it.
			losers = append(losers, rev)
			continue
		}
		if candidate.MTime > winner.MTime {
			losers = append(losers, winner.Rev)
			winner = *candidate
		} else {
			losers = append(losers, rev)
		}
	}

	// If the true winner differs from the CouchDB-chosen winner, write the
	// correct content as a new child of the current winning revision, then
	// tombstone the replaced branch.
	if winner.Rev != doc.Rev {
		winnerContent := winner
		winnerContent.Rev = doc.Rev // parent = current CouchDB winner
		newRev, err := s.db.PutMeta(ctx, &winnerContent)
		if err != nil {
			return doc, fmt.Errorf("resolveConflicts: rewrite %q: %w", doc.ID, err)
		}
		winner.Rev = newRev
	}

	// Tombstone all losing branches (best-effort; ignore errors).
	for _, rev := range losers {
		_ = s.db.DeleteRevision(ctx, doc.ID, rev)
	}

	winner.Conflicts = nil
	return winner, nil
}

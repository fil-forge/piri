package allocationstore

import (
	"context"
	"errors"
	"fmt"
	"iter"

	"github.com/fil-forge/libforge/commands/blob"
	"github.com/fil-forge/ucantone/did"
	"github.com/fil-forge/ucantone/ucan"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/ipfs/go-cid"
	"github.com/multiformats/go-multihash"
	"github.com/yugabyte/pgx/v5"

	"github.com/fil-forge/piri/pkg/database/harmonytx"
	"github.com/fil-forge/piri/pkg/store"
	"github.com/fil-forge/piri/pkg/store/allocationstore/allocation"
)

// PostgresStore implements AllocationStore on the ucan_allocations and
// ucan_pending_allocations tables. A change that reads an allocation first
// locks its row, so it acts on the allocation it read, in any process.
type PostgresStore struct {
	db *harmonydb.DB
}

var _ AllocationStore = (*PostgresStore)(nil)

// NewPostgresStore returns an AllocationStore on db.
func NewPostgresStore(db *harmonydb.DB) *PostgresStore {
	return &PostgresStore{db: db}
}

// listPendingPage is how many pending allocations ListPending reads per query.
const listPendingPage = 1000

func (s *PostgresStore) Get(ctx context.Context, digest multihash.Multihash, space did.DID) (alloc allocation.Allocation, err error) {
	err = harmonytx.Run(ctx, s.db, func(ctx context.Context, tx *harmonydb.Tx) error {
		alloc, err = scanAllocation(tx.QueryRow(`
			SELECT digest, space, size, expires, cause, allocation
			FROM ucan_allocations WHERE digest = $1 AND space = $2`,
			[]byte(digest), space.String()))
		return err
	})
	if err != nil {
		return allocation.Allocation{}, fmt.Errorf("getting allocation: %w", err)
	}
	return alloc, nil
}

func (s *PostgresStore) GetAnyNonExpired(ctx context.Context, digest multihash.Multihash, now ucan.UnixTimestamp) (alloc allocation.Allocation, err error) {
	err = harmonytx.Run(ctx, s.db, func(ctx context.Context, tx *harmonydb.Tx) error {
		alloc, err = scanAllocation(tx.QueryRow(`
			SELECT digest, space, size, expires, cause, allocation
			FROM ucan_allocations WHERE digest = $1 AND expires > $2 LIMIT 1`,
			[]byte(digest), int64(now)))
		return err
	})
	if err != nil {
		return allocation.Allocation{}, fmt.Errorf("getting non-expired allocation: %w", err)
	}
	return alloc, nil
}

func (s *PostgresStore) Exists(ctx context.Context, digest multihash.Multihash) (exists bool, err error) {
	err = harmonytx.Run(ctx, s.db, func(ctx context.Context, tx *harmonydb.Tx) error {
		return tx.QueryRow(`SELECT EXISTS (SELECT 1 FROM ucan_allocations WHERE digest = $1)`,
			[]byte(digest)).Scan(&exists)
	})
	if err != nil {
		return false, fmt.Errorf("checking allocation exists: %w", err)
	}
	return exists, nil
}

func (s *PostgresStore) Put(ctx context.Context, alloc allocation.Allocation) error {
	err := harmonytx.Run(ctx, s.db, func(ctx context.Context, tx *harmonydb.Tx) error {
		_, err := tx.Exec(`
			INSERT INTO ucan_allocations (digest, space, size, expires, cause, allocation)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (digest, space) DO UPDATE SET
				size = EXCLUDED.size, expires = EXCLUDED.expires,
				cause = EXCLUDED.cause, allocation = EXCLUDED.allocation`,
			allocationArgs(alloc)...)
		return err
	})
	if err != nil {
		return fmt.Errorf("putting allocation: %w", err)
	}
	return nil
}

func (s *PostgresStore) Claim(ctx context.Context, alloc allocation.Allocation) (claimed bool, err error) {
	err = harmonytx.Run(ctx, s.db, func(ctx context.Context, tx *harmonydb.Tx) error {
		n, err := tx.Exec(`
			INSERT INTO ucan_allocations (digest, space, size, expires, cause, allocation)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (digest, space) DO NOTHING`,
			allocationArgs(alloc)...)
		claimed = n == 1
		return err
	})
	if err != nil {
		return false, fmt.Errorf("claiming allocation: %w", err)
	}
	return claimed, nil
}

func (s *PostgresStore) MakeCurrent(ctx context.Context, digest multihash.Multihash, space did.DID, link cid.Cid) error {
	err := harmonytx.Run(ctx, s.db, func(ctx context.Context, tx *harmonydb.Tx) error {
		_, err := tx.Exec(`
			UPDATE ucan_allocations SET allocation = $3
			WHERE digest = $1 AND space = $2 AND allocation IS DISTINCT FROM $3`,
			[]byte(digest), space.String(), link.String())
		return err
	})
	if err != nil {
		return fmt.Errorf("making allocation current: %w", err)
	}
	return nil
}

func (s *PostgresStore) Delete(ctx context.Context, digest multihash.Multihash, space did.DID) error {
	err := harmonytx.Run(ctx, s.db, func(ctx context.Context, tx *harmonydb.Tx) error {
		_, err := tx.Exec(`DELETE FROM ucan_allocations WHERE digest = $1 AND space = $2`,
			[]byte(digest), space.String())
		return err
	})
	if err != nil {
		return fmt.Errorf("deleting allocation: %w", err)
	}
	return nil
}

func (s *PostgresStore) ReleaseByTask(ctx context.Context, link cid.Cid) (released bool, err error) {
	err = harmonytx.Run(ctx, s.db, func(ctx context.Context, tx *harmonydb.Tx) error {
		released = false
		alloc, err := scanAllocation(tx.QueryRow(`
			SELECT digest, space, size, expires, cause, allocation
			FROM ucan_allocations WHERE allocation = $1 FOR UPDATE`,
			link.String()))
		if errors.Is(err, store.ErrNotFound) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("getting allocation by allocate task: %w", err)
		}
		if err := handOverClaim(tx, link, alloc); err != nil {
			return err
		}
		released = true
		return nil
	})
	if err != nil {
		return false, err
	}
	return released, nil
}

func (s *PostgresStore) GetByTask(ctx context.Context, link cid.Cid) (alloc allocation.Allocation, err error) {
	err = harmonytx.Run(ctx, s.db, func(ctx context.Context, tx *harmonydb.Tx) error {
		alloc, err = scanAllocation(tx.QueryRow(`
			SELECT digest, space, size, expires, cause, allocation
			FROM ucan_allocations WHERE allocation = $1`,
			link.String()))
		return err
	})
	if err != nil {
		return allocation.Allocation{}, fmt.Errorf("getting allocation by allocate task: %w", err)
	}
	return alloc, nil
}

func (s *PostgresStore) ListSpaces(ctx context.Context, digest multihash.Multihash) (spaces []did.DID, err error) {
	err = harmonytx.Run(ctx, s.db, func(ctx context.Context, tx *harmonydb.Tx) error {
		rows, err := tx.Query(`SELECT space FROM ucan_allocations WHERE digest = $1`, []byte(digest))
		if err != nil {
			return err
		}
		spaces, err = store.ScanSpaces(rows)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("listing allocations: %w", err)
	}
	return spaces, nil
}

func (s *PostgresStore) PutPending(ctx context.Context, p allocation.Pending) error {
	err := harmonytx.Run(ctx, s.db, func(ctx context.Context, tx *harmonydb.Tx) error {
		_, err := tx.Exec(`
			INSERT INTO ucan_pending_allocations
				(allocation, space, size, digest_code, cause, expires, upload_id, digest)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			ON CONFLICT (allocation) DO UPDATE SET
				space = EXCLUDED.space, size = EXCLUDED.size,
				digest_code = EXCLUDED.digest_code, cause = EXCLUDED.cause,
				expires = EXCLUDED.expires, upload_id = EXCLUDED.upload_id,
				digest = EXCLUDED.digest`,
			p.Allocation.String(), p.Space.String(), int64(p.Size), int64(p.DigestCode),
			p.Cause.String(), int64(p.Expires), p.UploadID, nullableDigest(p.Digest))
		return err
	})
	if err != nil {
		return fmt.Errorf("putting pending allocation: %w", err)
	}
	return nil
}

func (s *PostgresStore) GetPending(ctx context.Context, link cid.Cid) (p allocation.Pending, err error) {
	err = harmonytx.Run(ctx, s.db, func(ctx context.Context, tx *harmonydb.Tx) error {
		p, err = scanPending(tx.QueryRow(`
			SELECT allocation, space, size, digest_code, cause, expires, upload_id, digest
			FROM ucan_pending_allocations WHERE allocation = $1`,
			link.String()))
		return err
	})
	if err != nil {
		return allocation.Pending{}, fmt.Errorf("getting pending allocation: %w", err)
	}
	return p, nil
}

func (s *PostgresStore) DeletePending(ctx context.Context, link cid.Cid) error {
	err := harmonytx.Run(ctx, s.db, func(ctx context.Context, tx *harmonydb.Tx) error {
		_, err := tx.Exec(`DELETE FROM ucan_pending_allocations WHERE allocation = $1`, link.String())
		return err
	})
	if err != nil {
		return fmt.Errorf("deleting pending allocation: %w", err)
	}
	return nil
}

// Lock takes a transaction-level advisory lock on (digest, space), which
// holds whether or not an allocation row exists: a row lock would hold
// nothing for a space with no allocation of the blob. Two pairs whose keys
// hash alike only wait on each other.
func (s *PostgresStore) Lock(ctx context.Context, digest multihash.Multihash, space did.DID) error {
	err := harmonytx.Run(ctx, s.db, func(ctx context.Context, tx *harmonydb.Tx) error {
		_, err := tx.Exec(`
			SELECT pg_advisory_xact_lock(hashtextextended('ucan_allocations:' || encode($1, 'hex') || ':' || $2, 0))`,
			[]byte(digest), space.String())
		return err
	})
	if err != nil {
		return fmt.Errorf("locking allocation: %w", err)
	}
	return nil
}

// LockBlob takes a transaction-level advisory lock on the digest.
func (s *PostgresStore) LockBlob(ctx context.Context, digest multihash.Multihash) error {
	err := harmonytx.Run(ctx, s.db, func(ctx context.Context, tx *harmonydb.Tx) error {
		_, err := tx.Exec(`
			SELECT pg_advisory_xact_lock(hashtextextended('blob:' || encode($1, 'hex'), 0))`,
			[]byte(digest))
		return err
	})
	if err != nil {
		return fmt.Errorf("locking blob: %w", err)
	}
	return nil
}

// LockPending takes a transaction-level advisory lock on the allocate task
// link, which holds whether or not a pending allocation exists for it, so an
// allocate of the link serializes with a reject or a retry of it.
func (s *PostgresStore) LockPending(ctx context.Context, link cid.Cid) error {
	err := harmonytx.Run(ctx, s.db, func(ctx context.Context, tx *harmonydb.Tx) error {
		_, err := tx.Exec(`
			SELECT pg_advisory_xact_lock(hashtextextended('ucan_pending_allocations:' || $1, 0))`,
			link.String())
		return err
	})
	if err != nil {
		return fmt.Errorf("locking pending allocation: %w", err)
	}
	return nil
}

// ListPending reads a page at a time, each in its own transaction unless ctx
// carries one, so the caller may change the store while it iterates.
func (s *PostgresStore) ListPending(ctx context.Context) iter.Seq2[allocation.Pending, error] {
	return s.listPending(ctx, nil)
}

// ListExpiredPending reads as ListPending does.
func (s *PostgresStore) ListExpiredPending(ctx context.Context, now ucan.UnixTimestamp) iter.Seq2[allocation.Pending, error] {
	expires := int64(now)
	return s.listPending(ctx, &expires)
}

// listPending lists the pending allocations, those expiring at or before
// expiresBy when it is set, in pages ordered by allocate task.
func (s *PostgresStore) listPending(ctx context.Context, expiresBy *int64) iter.Seq2[allocation.Pending, error] {
	return func(yield func(allocation.Pending, error) bool) {
		after := ""
		for {
			var page []allocation.Pending
			err := harmonytx.Run(ctx, s.db, func(ctx context.Context, tx *harmonydb.Tx) error {
				page = page[:0]
				rows, err := tx.Query(`
					SELECT allocation, space, size, digest_code, cause, expires, upload_id, digest
					FROM ucan_pending_allocations
					WHERE allocation > $1 AND ($2::bigint IS NULL OR expires <= $2)
					ORDER BY allocation LIMIT $3`,
					after, expiresBy, listPendingPage)
				if err != nil {
					return err
				}
				defer rows.Close()
				for rows.Next() {
					p, err := scanPending(rows)
					if err != nil {
						return err
					}
					page = append(page, p)
				}
				return rows.Err()
			})
			if err != nil {
				yield(allocation.Pending{}, fmt.Errorf("listing pending allocations: %w", err))
				return
			}
			for _, p := range page {
				if !yield(p, nil) {
					return
				}
			}
			if len(page) < listPendingPage {
				return
			}
			after = page[len(page)-1].Allocation.String()
		}
	}
}

// ReleasePending takes the allocate task link's lock and re-reads the record
// under it: the upload may have recorded its digest since p was read. Once the
// data was received, the blob in the space is locked before the claim is
// handed over, so an accept of the allocation, or a concurrent release of
// another upload of the same content, runs before or after it, never
// between. It takes these locks itself, in the order [AllocationStore.Lock]
// describes, so every caller holds them; a caller that already holds them
// in the same transaction takes them again without waiting.
func (s *PostgresStore) ReleasePending(ctx context.Context, p allocation.Pending) (allocation.Pending, error) {
	released := p
	err := harmonytx.Run(ctx, s.db, func(ctx context.Context, tx *harmonydb.Tx) error {
		released = p
		if err := s.LockPending(ctx, p.Allocation); err != nil {
			return err
		}
		latest, err := scanPending(tx.QueryRow(`
			SELECT allocation, space, size, digest_code, cause, expires, upload_id, digest
			FROM ucan_pending_allocations WHERE allocation = $1 FOR UPDATE`,
			p.Allocation.String()))
		if err == nil {
			released = latest
		} else if !errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("getting pending allocation: %w", err)
		}
		if len(released.Digest) > 0 {
			if err := s.Lock(ctx, released.Digest, released.Space); err != nil {
				return err
			}
			alloc, err := scanAllocation(tx.QueryRow(`
				SELECT digest, space, size, expires, cause, allocation
				FROM ucan_allocations WHERE digest = $1 AND space = $2 FOR UPDATE`,
				[]byte(released.Digest), released.Space.String()))
			switch {
			case errors.Is(err, store.ErrNotFound):
			case err != nil:
				return fmt.Errorf("getting allocation: %w", err)
			case alloc.Allocation == released.Allocation:
				if err := handOverClaim(tx, released.Allocation, alloc); err != nil {
					return err
				}
			}
		}
		if _, err := tx.Exec(`DELETE FROM ucan_pending_allocations WHERE allocation = $1`,
			released.Allocation.String()); err != nil {
			return fmt.Errorf("deleting pending allocation: %w", err)
		}
		return nil
	})
	return released, err
}

// handOverClaim gives the (digest, space) allocation that the task link
// released held to another received upload of the same content in the same
// space, or deletes it when there is none. The caller holds the allocation's
// row lock.
func handOverClaim(tx *harmonydb.Tx, released cid.Cid, alloc allocation.Allocation) error {
	other, err := scanPending(tx.QueryRow(`
		SELECT allocation, space, size, digest_code, cause, expires, upload_id, digest
		FROM ucan_pending_allocations
		WHERE digest = $1 AND space = $2 AND allocation <> $3
		LIMIT 1`,
		[]byte(alloc.Blob.Digest), alloc.Space.String(), released.String()))
	if errors.Is(err, store.ErrNotFound) {
		if _, err := tx.Exec(`DELETE FROM ucan_allocations WHERE digest = $1 AND space = $2`,
			[]byte(alloc.Blob.Digest), alloc.Space.String()); err != nil {
			return fmt.Errorf("deleting allocation: %w", err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("finding upload to hand allocation over to: %w", err)
	}
	if _, err := tx.Exec(`
		UPDATE ucan_allocations SET cause = $3, allocation = $4, expires = $5
		WHERE digest = $1 AND space = $2`,
		[]byte(alloc.Blob.Digest), alloc.Space.String(),
		other.Cause.String(), other.Allocation.String(), int64(other.Expires)); err != nil {
		return fmt.Errorf("handing over allocation: %w", err)
	}
	return nil
}

func allocationArgs(alloc allocation.Allocation) []any {
	return []any{
		[]byte(alloc.Blob.Digest), alloc.Space.String(), int64(alloc.Blob.Size),
		int64(alloc.Expires), alloc.Cause.String(), nullableCID(alloc.Allocation),
	}
}

func scanAllocation(r store.Row) (allocation.Allocation, error) {
	var (
		digest         []byte
		space, cause   string
		size, expires  int64
		allocationLink *string
	)
	if err := r.Scan(&digest, &space, &size, &expires, &cause, &allocationLink); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return allocation.Allocation{}, store.ErrNotFound
		}
		return allocation.Allocation{}, err
	}
	spaceDID, err := did.Parse(space)
	if err != nil {
		return allocation.Allocation{}, fmt.Errorf("parsing space: %w", err)
	}
	causeLink, err := cid.Parse(cause)
	if err != nil {
		return allocation.Allocation{}, fmt.Errorf("parsing cause: %w", err)
	}
	alloc := allocation.Allocation{
		Space:   spaceDID,
		Blob:    blob.Blob{Digest: digest, Size: uint64(size)},
		Expires: ucan.UnixTimestamp(expires),
		Cause:   causeLink,
	}
	if allocationLink != nil {
		if alloc.Allocation, err = cid.Parse(*allocationLink); err != nil {
			return allocation.Allocation{}, fmt.Errorf("parsing allocation: %w", err)
		}
	}
	return alloc, nil
}

type pendingRow struct {
	Allocation string
	Space      string
	Size       int64
	DigestCode int64
	Cause      string
	Expires    int64
	UploadID   string
	Digest     []byte
}

func (r pendingRow) pending() (allocation.Pending, error) {
	link, err := cid.Parse(r.Allocation)
	if err != nil {
		return allocation.Pending{}, fmt.Errorf("parsing allocation: %w", err)
	}
	space, err := did.Parse(r.Space)
	if err != nil {
		return allocation.Pending{}, fmt.Errorf("parsing space: %w", err)
	}
	cause, err := cid.Parse(r.Cause)
	if err != nil {
		return allocation.Pending{}, fmt.Errorf("parsing cause: %w", err)
	}
	p := allocation.Pending{
		Allocation: link,
		Space:      space,
		Size:       uint64(r.Size),
		DigestCode: uint64(r.DigestCode),
		Cause:      cause,
		Expires:    ucan.UnixTimestamp(r.Expires),
		UploadID:   r.UploadID,
	}
	if len(r.Digest) > 0 {
		p.Digest = r.Digest
	}
	return p, nil
}

func scanPending(r store.Row) (allocation.Pending, error) {
	var pr pendingRow
	err := r.Scan(&pr.Allocation, &pr.Space, &pr.Size, &pr.DigestCode, &pr.Cause, &pr.Expires, &pr.UploadID, &pr.Digest)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return allocation.Pending{}, store.ErrNotFound
		}
		return allocation.Pending{}, err
	}
	return pr.pending()
}

func nullableCID(c cid.Cid) *string {
	if !c.Defined() {
		return nil
	}
	s := c.String()
	return &s
}

func nullableDigest(d multihash.Multihash) []byte {
	if len(d) == 0 {
		return nil
	}
	return d
}

package store

import (
	"fmt"

	"github.com/fil-forge/ucantone/did"
)

// Row is a result row of a Postgres store's query.
type Row interface {
	Scan(dest ...any) error
}

// Rows is the result of a Postgres store's query of several rows.
type Rows interface {
	Row
	Next() bool
	Err() error
	Close()
}

// ScanSpaces reads a query of one column, a space DID per row, and closes it.
func ScanSpaces(rows Rows) ([]did.DID, error) {
	defer rows.Close()
	spaces := []did.DID{}
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		space, err := did.Parse(s)
		if err != nil {
			return nil, fmt.Errorf("parsing space: %w", err)
		}
		spaces = append(spaces, space)
	}
	return spaces, rows.Err()
}

package ucan

import (
	"fmt"
)

// All error types in this file implement [errors.Named] (a single
// `Name() string` accessor). `*execution.Response.SetFailure` uses the name
// when issuing a failure receipt.

type BlobSizeLimitExceededError struct {
	size uint64
	max  uint64
}

func (be BlobSizeLimitExceededError) Name() string {
	return "BlobSizeOutsideOfSupportedRange"
}

func (be BlobSizeLimitExceededError) Error() string {
	return fmt.Sprintf("Blob of %d bytes, exceeds size limit of %d bytes", be.size, be.max)
}

func NewBlobSizeLimitExceededError(size uint64, max uint64) BlobSizeLimitExceededError {
	return BlobSizeLimitExceededError{size, max}
}

type AllocatedMemoryNotWrittenError struct{}

func (ae AllocatedMemoryNotWrittenError) Name() string {
	return "AllocatedMemoryHadNotBeenWrittenTo"
}

func (ae AllocatedMemoryNotWrittenError) Error() string {
	return "Blob not found"
}

func NewAllocatedMemoryNotWrittenError() AllocatedMemoryNotWrittenError {
	return AllocatedMemoryNotWrittenError{}
}

// NotMigratedError marks a handler whose body remains stubbed because its
// semantics depend on an interface that hasn't migrated yet (typically a
// capability in a peer service, not a missing piri-side primitive).
type NotMigratedError struct {
	Command string
}

func (e NotMigratedError) Name() string {
	return "HandlerNotMigrated"
}

func (e NotMigratedError) Error() string {
	return fmt.Sprintf("%s handler awaits an upstream interface that has not yet migrated to UCAN 1.0", e.Command)
}

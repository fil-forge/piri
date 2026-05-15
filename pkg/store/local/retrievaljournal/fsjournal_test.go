package retrievaljournal

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"testing"

	contentcaps "github.com/fil-forge/libforge/capabilities/content"
	"github.com/fil-forge/libforge/testutil"
	"github.com/fil-forge/ucantone/ucan/invocation"
	"github.com/fil-forge/ucantone/ucan/receipt"
	"github.com/ipfs/go-cid"
	"github.com/multiformats/go-multicodec"
	"github.com/multiformats/go-multihash"
	"github.com/stretchr/testify/require"
)

func createTestReceipt(t *testing.T) *receipt.Receipt {
	client := testutil.Alice
	node := testutil.RandomSigner(t)
	space := testutil.RandomDID(t)
	inv, err := contentcaps.Retrieve.Invoke(
		client,
		space,
		&contentcaps.RetrieveArguments{
			Blob:  contentcaps.Blob{Digest: testutil.RandomMultihash(t)},
			Range: contentcaps.RetrieveArguments{}.Range, // zero range
		},
		invocation.WithAudience(node.DID()),
	)
	require.NoError(t, err)

	rcpt, err := receipt.IssueOK(node, inv.Link(), &contentcaps.RetrieveOK{})
	require.NoError(t, err)
	return rcpt
}

func TestAppend(t *testing.T) {
	t.Run("happy path", func(t *testing.T) {
		tempDir := t.TempDir()

		journal, err := NewFSJournal(tempDir, 0)
		require.NoError(t, err)

		rcpt := createTestReceipt(t)

		_, _, err = journal.Append(t.Context(), rcpt)
		require.NoError(t, err)

		files, err := filepath.Glob(filepath.Join(tempDir, currentBatchName))
		require.NoError(t, err)
		require.Len(t, files, 1, "expected one batch file")

		fileInfo, err := os.Stat(files[0])
		require.NoError(t, err)
		require.True(t, fileInfo.Size() > 0, "batch file should not be empty")

		data, err := os.ReadFile(files[0])
		require.NoError(t, err)
		require.True(t, len(data) > 0, "should be able to read file content")
	})

	t.Run("batch is rotated when max batch size is reached", func(t *testing.T) {
		tempDir := t.TempDir()

		journal, err := NewFSJournal(tempDir, 1024)
		require.NoError(t, err)

		var rcpts []*receipt.Receipt
		for range 10 {
			rcpts = append(rcpts, createTestReceipt(t))
		}

		numBatches := 0
		currentBatchSize := 18 // CAR header
		for _, rcpt := range rcpts {
			batchRotated, _, err := journal.Append(t.Context(), rcpt)
			require.NoError(t, err)

			// Each journal entry is the receipt envelope bytes + CAR block
			// overhead (CID prefix + length varint).
			currentBatchSize += len(rcpt.Bytes()) + 39

			if int64(currentBatchSize) >= journal.maxBatchSize {
				require.True(t, batchRotated)
				currentBatchSize = 18
				numBatches++
				require.Equal(t, int64(currentBatchSize), journal.currSize)

				files, err := filepath.Glob(filepath.Join(tempDir, batchFilePrefix+"*"+batchFileSuffix))
				require.NoError(t, err)
				require.Len(t, files, numBatches, "expected %d completed batch files", numBatches)
			} else {
				require.False(t, batchRotated)
			}
		}
	})

	t.Run("concurrent append", func(t *testing.T) {
		tempDir := t.TempDir()

		journal, err := NewFSJournal(tempDir, 1024)
		require.NoError(t, err)

		var wg sync.WaitGroup
		numReceipts := 10

		numBatches := atomic.Int32{}
		for range numReceipts {
			wg.Add(1)
			go func() {
				defer wg.Done()
				rcpt := createTestReceipt(t)
				batchRotated, _, err := journal.Append(t.Context(), rcpt)
				require.NoError(t, err)

				if batchRotated {
					numBatches.Add(1)
				}
			}()
		}

		wg.Wait()

		files, err := filepath.Glob(filepath.Join(tempDir, batchFilePrefix+"*"+batchFileSuffix))
		require.NoError(t, err)
		n := int(numBatches.Load())
		require.Len(t, files, n, "expected %d completed batch files", n)
	})

	t.Run("fails with nil receipt", func(t *testing.T) {
		tempDir := t.TempDir()

		journal, err := NewFSJournal(tempDir, 0)
		require.NoError(t, err)

		_, _, err = journal.Append(t.Context(), nil)
		require.Error(t, err)
	})
}

func TestRotate(t *testing.T) {
	t.Run("happy path", func(t *testing.T) {
		tempDir := t.TempDir()

		journal, err := NewFSJournal(tempDir, 0)
		require.NoError(t, err)

		rcpt := createTestReceipt(t)
		batchRotated, _, err := journal.Append(t.Context(), rcpt)
		require.NoError(t, err)
		require.False(t, batchRotated)

		rotatedBatchCID, err := journal.rotate()
		require.NoError(t, err)
		require.NotEmpty(t, rotatedBatchCID)

		files, err := filepath.Glob(filepath.Join(tempDir, fmt.Sprintf("%s%s%s", batchFilePrefix, rotatedBatchCID.String(), batchFileSuffix)))
		require.NoError(t, err)
		require.Len(t, files, 1, "expected one batch file")
	})

	t.Run("forces a rotation", func(t *testing.T) {
		tempDir := t.TempDir()

		journal, err := NewFSJournal(tempDir, 0)
		require.NoError(t, err)

		rcpt := createTestReceipt(t)
		batchRotated, _, err := journal.Append(t.Context(), rcpt)
		require.NoError(t, err)
		require.False(t, batchRotated)

		rotated, batchID, err := journal.ForceRotate(t.Context())
		require.NoError(t, err)
		require.True(t, rotated)
		require.NotEmpty(t, batchID)

		files, err := filepath.Glob(filepath.Join(tempDir, fmt.Sprintf("%s%s%s", batchFilePrefix, batchID.String(), batchFileSuffix)))
		require.NoError(t, err)
		require.Len(t, files, 1, "expected one batch file")
	})

	t.Run("does not force rotate if empty", func(t *testing.T) {
		tempDir := t.TempDir()

		journal, err := NewFSJournal(tempDir, 0)
		require.NoError(t, err)

		rotated, batchID, err := journal.ForceRotate(t.Context())
		require.NoError(t, err)
		require.False(t, rotated)
		require.Equal(t, cid.Undef, batchID)
	})
}

func TestGetBatch(t *testing.T) {
	t.Run("happy path", func(t *testing.T) {
		tempDir := t.TempDir()

		journal, err := NewFSJournal(tempDir, 100)
		require.NoError(t, err)

		rcpt := createTestReceipt(t)
		batchRotated, rotatedBatchCID, err := journal.Append(t.Context(), rcpt)
		require.NoError(t, err)
		require.True(t, batchRotated)

		f, err := os.Open(filepath.Join(tempDir, fmt.Sprintf("egress.%s.car", rotatedBatchCID.String())))
		require.NoError(t, err)
		readBytes, err := io.ReadAll(f)
		require.NoError(t, err)

		batch, err := journal.GetBatch(t.Context(), rotatedBatchCID)
		require.NoError(t, err)

		batchBytes, err := io.ReadAll(batch)
		require.NoError(t, err)

		require.True(t, slices.Equal(readBytes, batchBytes))
	})
}

func TestResumeAfterRestart(t *testing.T) {
	t.Run("hash is computed correctly after restart", func(t *testing.T) {
		tempDir := t.TempDir()

		journal1, err := NewFSJournal(tempDir, DefaultBatchSize)
		require.NoError(t, err)

		rcpt1 := createTestReceipt(t)
		rcpt2 := createTestReceipt(t)

		rotated, batch, err := journal1.Append(t.Context(), rcpt1)
		require.NoError(t, err)
		require.False(t, rotated)
		require.Equal(t, cid.Undef, batch)

		rotated, batch, err = journal1.Append(t.Context(), rcpt2)
		require.NoError(t, err)
		require.False(t, rotated)
		require.Equal(t, cid.Undef, batch)

		err = journal1.Close()
		require.NoError(t, err)

		journal2, err := NewFSJournal(tempDir, DefaultBatchSize)
		require.NoError(t, err)

		rcpt3 := createTestReceipt(t)
		rotated, batch, err = journal2.Append(t.Context(), rcpt3)
		require.NoError(t, err)
		require.False(t, rotated)
		require.Equal(t, cid.Undef, batch)

		rotatedBatchCID, err := journal2.rotate()
		require.NoError(t, err)

		batchReader, err := journal2.GetBatch(t.Context(), rotatedBatchCID)
		require.NoError(t, err)
		defer batchReader.Close()

		batchData, err := io.ReadAll(batchReader)
		require.NoError(t, err)

		expectedHash := sha256.Sum256(batchData)
		expectedMhBytes, _ := multihash.Encode(expectedHash[:], multihash.SHA2_256)
		expectedMh := multihash.Multihash(expectedMhBytes)
		expectedCID := cid.NewCidV1(uint64(multicodec.Car), expectedMh)

		require.Equal(t, expectedCID.String(), rotatedBatchCID.String(),
			"batch CID should be the hash of the entire file, including data written before restart")

		err = journal2.Close()
		require.NoError(t, err)
	})
}

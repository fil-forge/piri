package verifyread

import (
	"bytes"
	"crypto/md5"
	"crypto/sha256"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVerifyRead(t *testing.T) {
	// Helper to create test data with known hash
	createTestData := func(content string) (data []byte, hash []byte) {
		data = []byte(content)
		h := sha256.Sum256(data)
		return data, h[:]
	}

	t.Run("successful validation", func(t *testing.T) {
		// Create test data
		data, expectedHash := createTestData("Hello, World!")
		source := bytes.NewReader(data)

		// Create validating reader
		reader, err := New(
			source,
			sha256.New(),
			expectedHash,
		)
		require.NoError(t, err)

		// Consumer reads all data
		result := &bytes.Buffer{}
		n, err := io.Copy(result, reader)

		// Should succeed
		assert.NoError(t, err)
		assert.Equal(t, int64(len(data)), n)
		assert.Equal(t, data, result.Bytes())
		assert.Equal(t, uint64(len(data)), reader.BytesRead())
	})

	t.Run("hash mismatch causes consumer failure", func(t *testing.T) {
		// Create test data
		data, _ := createTestData("Hello, World!")
		source := bytes.NewReader(data)

		// Use wrong hash
		wrongHash := sha256.Sum256([]byte("Different content"))

		// Create validating reader with wrong hash
		reader, err := New(
			source,
			sha256.New(),
			wrongHash[:],
		)
		require.NoError(t, err)

		// Consumer tries to read all data
		result := &bytes.Buffer{}
		n, err := io.Copy(result, reader)

		// Should FAIL with hash validation error
		assert.ErrorIs(t, err, ErrHashMismatch)

		// Note: io.Copy may have written bytes before the error was detected at EOF.
		// This is expected behavior - validation happens after all data is read.
		// The important thing is that the operation returns an error, and the
		// consumer should discard/cleanup the partially written data.
		assert.Equal(t, int64(len(data)), n)  // Bytes were written before validation failed
		assert.Equal(t, data, result.Bytes()) // Data was copied but operation failed
	})

	t.Run("partial reads", func(t *testing.T) {
		// Test that validation happens even with small buffer reads
		data, expectedHash := createTestData("Hello, World! This is a longer message.")
		source := bytes.NewReader(data)

		reader, err := New(
			source,
			sha256.New(),
			expectedHash,
		)
		require.NoError(t, err)

		// Read in small chunks
		result := &bytes.Buffer{}
		buf := make([]byte, 4) // Small 4-byte buffer

		for {
			n, err := reader.Read(buf)
			if n > 0 {
				result.Write(buf[:n])
			}
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		}

		assert.Equal(t, data, result.Bytes())
		assert.True(t, reader.Validated())
	})

	t.Run("chaining", func(t *testing.T) {
		data := bytes.Repeat([]byte("a"), 10*1024)

		shaDigest := sha256.Sum256(data)
		md5Digest := md5.Sum(data)

		reader0 := bytes.NewReader(data)

		reader1, err := New(
			reader0,
			sha256.New(),
			shaDigest[:],
		)
		require.NoError(t, err)

		reader2, _ := New(
			reader1,
			md5.New(),
			md5Digest[:])
		require.NoError(t, err)

		// Consumer reads all data
		result := &bytes.Buffer{}
		n, err := io.Copy(result, reader2)

		// Should succeed
		assert.NoError(t, err)
		assert.Equal(t, int64(len(data)), n)
		assert.Equal(t, data, result.Bytes())
		assert.Equal(t, uint64(len(data)), reader1.BytesRead())
		assert.Equal(t, uint64(len(data)), reader2.BytesRead())

	})
}

func BenchmarkHashValidatingReader(b *testing.B) {
	// Create 10MB of test data
	data := bytes.Repeat([]byte("a"), 10*1024*1024)
	hash := sha256.Sum256(data)

	b.Run("WithValidation", func(b *testing.B) {
		b.SetBytes(int64(len(data)))
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			reader, err := New(

				bytes.NewReader(data),
				sha256.New(),
				hash[:],
			)
			require.NoError(b, err)
			io.Copy(io.Discard, reader)
		}
	})

	b.Run("WithoutValidation", func(b *testing.B) {
		b.SetBytes(int64(len(data)))
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			reader := bytes.NewReader(data)
			io.Copy(io.Discard, reader)
		}
	})
}

// countingReader reports how much of the source was actually consumed, so an
// over-length test can assert the body was cut off rather than drained.
type countingReader struct {
	src  io.Reader
	read int
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.src.Read(p)
	c.read += n
	return n, err
}

func TestVerifyRead_ExpectedSize(t *testing.T) {
	data := []byte("Hello, World!")
	sum := sha256.Sum256(data)

	t.Run("exact size still validates", func(t *testing.T) {
		reader, err := New(bytes.NewReader(data), sha256.New(), sum[:],
			WithExpectedSize(uint64(len(data))))
		require.NoError(t, err)

		got, err := io.ReadAll(reader)
		assert.NoError(t, err)
		assert.Equal(t, data, got)
		assert.True(t, reader.Validated())
	})

	t.Run("over-length aborts without draining the source", func(t *testing.T) {
		// 1 MiB of body behind a declared size of 13 bytes: the read must
		// stop almost immediately rather than stream the whole thing.
		src := &countingReader{src: bytes.NewReader(bytes.Repeat([]byte("x"), 1<<20))}
		reader, err := New(src, sha256.New(), sum[:], WithExpectedSize(uint64(len(data))))
		require.NoError(t, err)

		_, err = io.ReadAll(reader)
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrSizeMismatch)
		assert.NotErrorIs(t, err, ErrHashMismatch, "size is the actionable diagnosis")
		assert.Less(t, src.read, 1<<20, "must not consume the whole over-long body")
	})

	t.Run("under-length reports size, not hash", func(t *testing.T) {
		// Before WithExpectedSize a truncated transfer surfaced as
		// ErrHashMismatch, which reads as data corruption rather than a
		// dropped connection.
		truncated := data[:5]
		reader, err := New(bytes.NewReader(truncated), sha256.New(), sum[:],
			WithExpectedSize(uint64(len(data))))
		require.NoError(t, err)

		_, err = io.ReadAll(reader)
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrSizeMismatch)
		assert.NotErrorIs(t, err, ErrHashMismatch)
		assert.Contains(t, err.Error(), "got 5")
	})

	t.Run("correct size but wrong bytes still fails the digest", func(t *testing.T) {
		wrong := []byte("Goodbye,World")
		require.Len(t, wrong, len(data))

		reader, err := New(bytes.NewReader(wrong), sha256.New(), sum[:],
			WithExpectedSize(uint64(len(data))))
		require.NoError(t, err)

		_, err = io.ReadAll(reader)
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrHashMismatch)
	})

	t.Run("without the option size is unenforced", func(t *testing.T) {
		// The pre-existing three-arg call site must keep its old behavior.
		reader, err := New(bytes.NewReader(data), sha256.New(), sum[:])
		require.NoError(t, err)

		_, err = io.ReadAll(reader)
		assert.NoError(t, err)
	})

	t.Run("error is latched across subsequent reads", func(t *testing.T) {
		reader, err := New(bytes.NewReader(data), sha256.New(), sum[:], WithExpectedSize(2))
		require.NoError(t, err)

		buf := make([]byte, 8)
		_, first := reader.Read(buf)
		require.ErrorIs(t, first, ErrSizeMismatch)

		_, second := reader.Read(buf)
		assert.ErrorIs(t, second, ErrSizeMismatch, "terminal error must stay latched")
	})
}

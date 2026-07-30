package aggregator

// DefaultMinAggregateSize is 128 MiB.
//
// This is a cost/latency choice, not a capacity one. Larger aggregates
// amortize the on-chain addRoots transaction over more pieces but make a
// blob wait longer before it is provable; smaller ones do the reverse.
const DefaultMinAggregateSize uint64 = 128 << 20

// MinAllowedAggregateSize is the floor for the configured threshold. Below
// this the on-chain cost per byte stops being amortized in any meaningful
// way.
const MinAllowedAggregateSize uint64 = 1 << 20

// MaxAllowedAggregateSize is the structural ceiling on the threshold.
//
// Append flushes once buffered pieces reach the threshold, and the piece that
// crosses it is itself at most one threshold in size (anything larger becomes
// its own aggregate immediately). So the worst-case aggregate is twice the
// threshold. NewAggregate zero-pads through
// zerocomm.PieceComms[TrailingZeros64(size)-7], a 35-entry table, which tops
// out at 2^41 — so a threshold of 2^40 puts the worst case exactly at that
// bound and anything larger could not be padded.
//
// This is far above any useful setting; the practical ceiling is how long an
// operator is willing to make a blob wait before it becomes provable.
const MaxAllowedAggregateSize uint64 = 1 << 40

// Policy is a read-through handle on the aggregation threshold, which an
// operator may retune at runtime. The zero Policy reports the default, so it
// is usable unset.
type Policy struct {
	read func() uint64
}

// NewPolicy builds a Policy from a function returning the current threshold.
// The function is called on every fold, not snapshotted.
func NewPolicy(read func() uint64) Policy {
	return Policy{read: read}
}

// MinAggregateSize is the padded size at which buffered pieces are folded
// into an aggregate.
func (p Policy) MinAggregateSize() uint64 {
	if p.read == nil {
		return DefaultMinAggregateSize
	}
	if v := p.read(); v > 0 {
		return v
	}
	return DefaultMinAggregateSize
}

package ingestion

// Cursor is opaque source-provider state. Sequence is Mindclade-owned and
// prevents a stale coordinator from moving a source cursor backwards.
type Cursor struct {
	Value    string
	Sequence uint64
}

func (c Cursor) Validate() error {
	if c.Value == "" || len(c.Value) > 1024 || c.Sequence == 0 {
		return invalid("cursor_invalid", "ingestion cursor is invalid", nil)
	}
	return nil
}
func (c Cursor) CanAdvance(next Cursor) bool {
	return c.Validate() == nil && next.Validate() == nil && next.Sequence > c.Sequence
}

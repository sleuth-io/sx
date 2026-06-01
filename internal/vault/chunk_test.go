package vault

import "testing"

func TestChunkSlice(t *testing.T) {
	// Boundaries that mirror the audit (5000) and usage (1000) chunk sizes:
	// exactly one over the size must produce two chunks with a 1-element tail.
	cases := []struct {
		n, size    int
		wantChunks []int // expected length of each chunk
	}{
		{0, 1000, nil},
		{1, 1000, []int{1}},
		{1000, 1000, []int{1000}},
		{1001, 1000, []int{1000, 1}},
		{5001, 5000, []int{5000, 1}},
		{10000, 5000, []int{5000, 5000}},
		{3, 0, []int{3}}, // non-positive size => single chunk
	}
	for _, c := range cases {
		items := make([]int, c.n)
		chunks := chunkSlice(items, c.size)
		if len(chunks) != len(c.wantChunks) {
			t.Fatalf("chunkSlice(n=%d,size=%d) => %d chunks, want %d", c.n, c.size, len(chunks), len(c.wantChunks))
		}
		total := 0
		for i, ch := range chunks {
			if len(ch) != c.wantChunks[i] {
				t.Errorf("chunkSlice(n=%d,size=%d) chunk[%d] len=%d, want %d", c.n, c.size, i, len(ch), c.wantChunks[i])
			}
			total += len(ch)
		}
		if total != c.n {
			t.Errorf("chunkSlice(n=%d,size=%d) covered %d elements, want %d", c.n, c.size, total, c.n)
		}
	}
}

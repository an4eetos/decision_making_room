package service

import "testing"

func TestChunkText(t *testing.T) {
	t.Parallel()

	short := "hello world"
	if chunks := ChunkText(short, 100); len(chunks) != 1 || chunks[0] != short {
		t.Fatalf("expected single chunk, got %v", chunks)
	}

	long := ""
	for i := 0; i < 500; i++ {
		long += "word "
	}
	chunks := ChunkText(long, 200)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
}

func TestParseTags(t *testing.T) {
	t.Parallel()

	tags := ParseTags("work, go,  architecture")
	if len(tags) != 3 || tags[0] != "work" || tags[2] != "architecture" {
		t.Fatalf("unexpected tags: %v", tags)
	}
}

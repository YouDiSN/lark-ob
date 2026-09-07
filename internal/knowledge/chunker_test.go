package knowledge

import "testing"

func TestChunkDocumentPreservesHeadingAndBlock(t *testing.T) {
	content := `<document><title block-id="title-1">发布手册</title><heading1 block-id="h1">生产发布</heading1><p block-id="p1">先完成代码审核。</p><p block-id="p2">再创建部署标签。</p></document>`
	chunks := ChunkDocument(content, 200)
	if len(chunks) == 0 {
		t.Fatal("expected chunks")
	}
	last := chunks[len(chunks)-1]
	if last.Heading != "生产发布" {
		t.Fatalf("heading = %q", last.Heading)
	}
	if last.BlockID != "h1" {
		t.Fatalf("block id = %q", last.BlockID)
	}
	if last.Content == "" || last.ContentHash == "" {
		t.Fatal("chunk content and hash must be populated")
	}
}

func TestChunkDocumentFallsBackForPlainText(t *testing.T) {
	chunks := ChunkDocument("plain knowledge", 200)
	if len(chunks) != 1 || chunks[0].Content != "plain knowledge" {
		t.Fatalf("unexpected chunks: %#v", chunks)
	}
}

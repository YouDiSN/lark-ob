package larksource

import (
	"context"
	"strings"
	"testing"
	"time"
)

type fakeRunner struct {
	response string
	args     []string
}

func (r *fakeRunner) Run(_ context.Context, args ...string) ([]byte, error) {
	r.args = append([]string(nil), args...)
	return []byte(r.response), nil
}

func TestFetchParsesDocumentEnvelope(t *testing.T) {
	runner := &fakeRunner{response: `{"ok":true,"identity":"user","data":{"document":{"document_id":"doc-1","revision_id":12,"content":"<title block-id=\"t1\">发布手册</title><p block-id=\"p1\">正文</p>"}}}`}
	source := New(runner)
	source.now = func() time.Time { return time.Unix(20, 0) }
	document, err := source.Fetch(context.Background(), "https://example.feishu.cn/docx/token")
	if err != nil {
		t.Fatal(err)
	}
	if document.DocumentID != "doc-1" || document.Revision != 12 || document.Title != "发布手册" {
		t.Fatalf("unexpected document: %#v", document)
	}
	joined := strings.Join(runner.args, " ")
	for _, expected := range []string{"docs +fetch", "--as user", "--doc-format xml", "--detail with-ids"} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("args %q do not include %q", joined, expected)
		}
	}
}

func TestFetchRejectsUntrustedURL(t *testing.T) {
	source := New(&fakeRunner{})
	_, err := source.Fetch(context.Background(), "https://example.com/docx/token")
	if err == nil {
		t.Fatal("expected URL validation error")
	}
}

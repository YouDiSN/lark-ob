package larksource

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/youdisn/lark-ob/internal/knowledge"
	"github.com/youdisn/lark-ob/internal/larkcli"
)

type Runner interface {
	Run(context.Context, ...string) ([]byte, error)
}

type CommandRunner struct {
	path string
}

func NewCommandRunner() (*CommandRunner, error) {
	path, err := larkcli.ResolvePath()
	if err != nil {
		return nil, fmt.Errorf("lark-cli 未安装: %w", err)
	}
	return &CommandRunner{path: path}, nil
}

func (r *CommandRunner) Run(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, r.path, args...)
	cmd.Env = append(os.Environ(),
		"LARKSUITE_CLI_NO_UPDATE_NOTIFIER=1",
		"LARKSUITE_CLI_NO_SKILLS_NOTIFIER=1",
	)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err == nil {
		return out, nil
	}
	message := strings.TrimSpace(stderr.String())
	var envelope struct {
		Error struct {
			Message string `json:"message"`
			Hint    string `json:"hint"`
		} `json:"error"`
	}
	if json.Unmarshal([]byte(message), &envelope) == nil && envelope.Error.Message != "" {
		message = envelope.Error.Message
		if envelope.Error.Hint != "" {
			message += "：" + envelope.Error.Hint
		}
	}
	if message == "" {
		message = err.Error()
	}
	return nil, fmt.Errorf("读取飞书文档失败: %s", message)
}

type Source struct {
	runner Runner
	now    func() time.Time
}

func New(runner Runner) *Source {
	return &Source{runner: runner, now: time.Now}
}

func (s *Source) Fetch(ctx context.Context, sourceURL string) (knowledge.Document, error) {
	sourceType, err := validateDocumentURL(sourceURL)
	if err != nil {
		return knowledge.Document{}, err
	}
	raw, err := s.runner.Run(ctx,
		"docs", "+fetch",
		"--as", "user",
		"--doc", sourceURL,
		"--doc-format", "xml",
		"--detail", "with-ids",
		"--format", "json",
	)
	if err != nil {
		return knowledge.Document{}, err
	}
	var envelope struct {
		OK   bool `json:"ok"`
		Data struct {
			Document struct {
				DocumentID string  `json:"document_id"`
				RevisionID flexInt `json:"revision_id"`
				Content    string  `json:"content"`
			} `json:"document"`
		} `json:"data"`
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return knowledge.Document{}, fmt.Errorf("解析 lark-cli 文档响应失败: %w", err)
	}
	if !envelope.OK {
		if envelope.Error.Message == "" {
			envelope.Error.Message = "lark-cli 返回未成功状态"
		}
		return knowledge.Document{}, fmt.Errorf("读取飞书文档失败: %s", envelope.Error.Message)
	}
	document := envelope.Data.Document
	if document.DocumentID == "" || strings.TrimSpace(document.Content) == "" {
		return knowledge.Document{}, fmt.Errorf("飞书文档响应缺少 document_id 或正文")
	}
	return knowledge.Document{
		SourceType: sourceType,
		URL:        sourceURL,
		Title:      knowledge.ExtractDocumentTitle(document.Content),
		DocumentID: document.DocumentID,
		Revision:   int64(document.RevisionID),
		Content:    document.Content,
		FetchedAt:  s.now(),
	}, nil
}

func validateDocumentURL(value string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" {
		return "", fmt.Errorf("请输入有效的 HTTPS 飞书文档链接")
	}
	host := strings.ToLower(parsed.Hostname())
	allowed := host == "feishu.cn" || strings.HasSuffix(host, ".feishu.cn") ||
		host == "larksuite.com" || strings.HasSuffix(host, ".larksuite.com") ||
		host == "doubao.com" || strings.HasSuffix(host, ".doubao.com")
	if !allowed {
		return "", fmt.Errorf("只支持飞书、Lark 或豆包文档域名")
	}
	path := strings.ToLower(parsed.Path)
	switch {
	case strings.Contains(path, "/docx/"):
		return "lark_doc", nil
	case strings.Contains(path, "/wiki/"):
		return "lark_wiki", nil
	default:
		return "", fmt.Errorf("当前只支持 /docx/ 和 /wiki/ 文档链接")
	}
}

type flexInt int64

func (v *flexInt) UnmarshalJSON(raw []byte) error {
	value := strings.Trim(string(raw), `"`)
	if value == "" || value == "null" {
		*v = 0
		return nil
	}
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return err
	}
	*v = flexInt(n)
	return nil
}

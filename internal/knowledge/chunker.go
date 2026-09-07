package knowledge

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"io"
	"regexp"
	"strings"
	"unicode"
)

type documentBlock struct {
	Kind    string
	Level   int
	BlockID string
	Text    string
}

type xmlFrame struct {
	name          string
	blockID       string
	text          strings.Builder
	hasBlockChild bool
}

var headingName = regexp.MustCompile(`^(?:heading|h)([1-6])$`)

func ChunkDocument(content string, maxRunes int) []Chunk {
	if maxRunes < 200 {
		maxRunes = 1400
	}
	blocks := parseDocumentBlocks(content)
	if len(blocks) == 0 {
		plain := cleanText(stripMarkup(content))
		if plain == "" {
			return nil
		}
		blocks = []documentBlock{{Kind: "paragraph", Text: plain}}
	}

	var chunks []Chunk
	var current strings.Builder
	var headingPath []string
	currentHeading := ""
	currentBlockID := ""
	flush := func() {
		text := strings.TrimSpace(current.String())
		if text == "" {
			return
		}
		sum := sha256.Sum256([]byte(currentHeading + "\n" + text))
		chunks = append(chunks, Chunk{
			Ordinal:     len(chunks),
			Heading:     currentHeading,
			BlockID:     currentBlockID,
			Content:     text,
			ContentHash: hex.EncodeToString(sum[:]),
		})
		current.Reset()
		currentBlockID = ""
	}

	for _, block := range blocks {
		if block.Text == "" {
			continue
		}
		if block.Level > 0 {
			flush()
			for len(headingPath) >= block.Level {
				headingPath = headingPath[:len(headingPath)-1]
			}
			headingPath = append(headingPath, block.Text)
			currentHeading = strings.Join(headingPath, " / ")
		}
		if currentBlockID == "" {
			currentBlockID = block.BlockID
		}
		if current.Len() > 0 && len([]rune(current.String()+"\n"+block.Text)) > maxRunes {
			flush()
			currentBlockID = block.BlockID
		}
		if current.Len() > 0 {
			current.WriteByte('\n')
		}
		current.WriteString(block.Text)
	}
	flush()
	return chunks
}

func ExtractDocumentTitle(content string) string {
	for _, block := range parseDocumentBlocks(content) {
		if block.Kind == "title" || block.Level > 0 {
			return block.Text
		}
	}
	return "未命名飞书文档"
}

func parseDocumentBlocks(content string) []documentBlock {
	decoder := xml.NewDecoder(strings.NewReader(content))
	decoder.Strict = false
	var stack []*xmlFrame
	var blocks []documentBlock
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil
		}
		switch value := token.(type) {
		case xml.StartElement:
			frame := &xmlFrame{name: strings.ToLower(value.Name.Local)}
			for _, attr := range value.Attr {
				name := strings.ToLower(attr.Name.Local)
				if name == "block-id" || name == "block_id" || name == "id" {
					frame.blockID = attr.Value
					break
				}
			}
			stack = append(stack, frame)
		case xml.CharData:
			text := string(value)
			for _, frame := range stack {
				frame.text.WriteString(text)
			}
		case xml.EndElement:
			if len(stack) == 0 {
				continue
			}
			frame := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			level := headingLevel(frame.name)
			if isBlockElement(frame.name) && (!frame.hasBlockChild || level > 0 || frame.name == "title") {
				text := cleanText(frame.text.String())
				if text != "" {
					blocks = append(blocks, documentBlock{Kind: frame.name, Level: level, BlockID: frame.blockID, Text: text})
				}
				if len(stack) > 0 {
					stack[len(stack)-1].hasBlockChild = true
				}
			}
		}
	}
	return blocks
}

func headingLevel(name string) int {
	match := headingName.FindStringSubmatch(name)
	if len(match) != 2 {
		return 0
	}
	return int(match[1][0] - '0')
}

func isBlockElement(name string) bool {
	if headingLevel(name) > 0 {
		return true
	}
	switch name {
	case "title", "p", "paragraph", "bullet", "ordered", "todo", "quote", "code", "callout", "text":
		return true
	default:
		return false
	}
}

func cleanText(value string) string {
	return strings.Join(strings.FieldsFunc(value, unicode.IsSpace), " ")
}

func stripMarkup(value string) string {
	var out strings.Builder
	inTag := false
	for _, r := range value {
		switch r {
		case '<':
			inTag = true
		case '>':
			inTag = false
			out.WriteByte(' ')
		default:
			if !inTag {
				out.WriteRune(r)
			}
		}
	}
	return out.String()
}

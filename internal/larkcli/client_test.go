package larkcli

import (
	"testing"
	"time"
)

func TestAuthStatusSupportsNestedUserIdentity(t *testing.T) {
	status, err := decodeAuthStatus([]byte(`{"identity":"user","identities":{"user":{"userName":"测试用户","openId":"ou_self"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if status.UserOpenID != "ou_self" || status.UserName != "测试用户" {
		t.Fatalf("nested identity was not normalized: %#v", status)
	}
}

func TestParseTimeSupportsMinutePrecisionFromCLI(t *testing.T) {
	got := parseTime("2026-03-30 02:21")
	want := time.Date(2026, 3, 30, 2, 21, 0, 0, time.UTC).UnixMilli()
	if got != want {
		t.Fatalf("parseTime() = %d, want %d", got, want)
	}
}

func TestParseTimeSupportsSecondsAndEpoch(t *testing.T) {
	if got := parseTime("2026-03-30 02:21:45"); got == 0 {
		t.Fatal("second precision time was not parsed")
	}
	if got := parseTime("1711765305"); got != 1711765305000 {
		t.Fatalf("seconds epoch = %d", got)
	}
}

func TestConvertMessagesUsesMessageChatIDTimestampAndPosition(t *testing.T) {
	message := cliMessage{MessageID: "m1", ChatID: "chat-from-message", MsgType: "text", CreateTime: "2026-03-30 02:21", MessagePosition: "42"}
	message.Sender.ID = "u1"
	message.Sender.Name = "成员"
	got := convertMessages([]cliMessage{message}, "fallback-chat")
	if len(got) != 1 || got[0].ChatID != "chat-from-message" || got[0].CreatedAt == 0 || got[0].Position != 42 {
		t.Fatalf("unexpected converted message: %#v", got)
	}
}

func TestConvertMessagesPreservesMentions(t *testing.T) {
	message := cliMessage{MessageID: "m1", ChatID: "chat-1", MsgType: "text", CreateTime: "1711765305"}
	message.Mentions = append(message.Mentions, struct {
		ID   string `json:"id"`
		Key  string `json:"key"`
		Name string `json:"name"`
	}{ID: "self", Key: "@_user_1", Name: "我"})
	got := convertMessages([]cliMessage{message}, "")
	if len(got) != 1 || len(got[0].Mentions) != 1 || got[0].Mentions[0].ID != "self" {
		t.Fatalf("mentions were not preserved: %#v", got)
	}
}

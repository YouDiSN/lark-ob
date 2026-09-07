package larkcli

import "testing"

func TestAvatarFromUserSearchMatchesOpenID(t *testing.T) {
	avatar, err := avatarFromUserSearch([]byte(`{"ok":true,"data":{"users":[{"open_id":"ou_other","avatar":{"avatar_72":"wrong"}},{"open_id":"ou_target","avatar":{"avatar_72":"https://example/avatar.png"}}]}}`), "ou_target")
	if err != nil {
		t.Fatal(err)
	}
	if avatar != "https://example/avatar.png" {
		t.Fatalf("avatar = %q", avatar)
	}
}

func TestProfileFromDirectorySearchUsesOnlyVisibleFields(t *testing.T) {
	profile, err := profileFromDirectorySearch([]byte(`{"ok":true,"data":{"users":[{"open_id":"ou_target","localized_name":"王卫","department":"技术-研发"}]}}`), "ou_target")
	if err != nil {
		t.Fatal(err)
	}
	if profile.Name != "王卫" || profile.Department != "技术-研发" || profile.Base != "" {
		t.Fatalf("profile = %#v", profile)
	}
}

func TestBaseFromDirectoryUserAllowsMissingConfidentialField(t *testing.T) {
	base, err := baseFromDirectoryUser([]byte(`{"ok":true,"data":{"user":{"open_id":"ou_target"}}}`), "ou_target")
	if err != nil || base != "" {
		t.Fatalf("base = %q, err = %v", base, err)
	}
}

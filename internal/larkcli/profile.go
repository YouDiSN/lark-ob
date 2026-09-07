package larkcli

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/youdisn/lark-ob/internal/model"
)

type userSearchEnvelope struct {
	Data struct {
		Users []struct {
			OpenID string `json:"open_id"`
			Name   string `json:"name"`
			Avatar struct {
				Avatar72 string `json:"avatar_72"`
			} `json:"avatar"`
		} `json:"users"`
	} `json:"data"`
}

type directorySearchEnvelope struct {
	Data struct {
		Users []struct {
			OpenID     string `json:"open_id"`
			Name       string `json:"localized_name"`
			Department string `json:"department"`
		} `json:"users"`
	} `json:"data"`
}

type directoryUserEnvelope struct {
	Data struct {
		User struct {
			OpenID string `json:"open_id"`
			City   string `json:"city"`
		} `json:"user"`
	} `json:"data"`
}

// FindUserAvatar resolves a visible internal user's Feishu avatar. The search
// API intentionally returns no matches for cross-tenant contacts, which the
// profile cache records as an empty avatar and the UI renders as initials.
func (c *Client) FindUserAvatar(ctx context.Context, userID, name string) (string, error) {
	profile, err := c.FindPersonProfile(ctx, userID, name)
	return profile.Avatar, err
}

// FindPersonProfile resolves only stable directory facts used by the inbox:
// avatar, work base and department. Missing scopes and confidential fields are
// optional enrichment failures and therefore return an otherwise valid partial
// profile instead of breaking message/profile processing.
func (c *Client) FindPersonProfile(ctx context.Context, userID, name string) (model.PersonProfile, error) {
	profile := model.PersonProfile{ID: strings.TrimSpace(userID), Name: strings.TrimSpace(name)}
	if strings.TrimSpace(userID) == "" || strings.TrimSpace(name) == "" {
		return profile, nil
	}
	params, err := json.Marshal(map[string]any{"query": name, "page_size": 20})
	if err != nil {
		return profile, err
	}
	raw, err := c.run(ctx, "api", "GET", "/open-apis/search/v1/user", "--as", "user", "--params", string(params), "--format", "json")
	if err == nil {
		profile.Avatar, _ = avatarFromUserSearch(raw, userID)
	} else if ctx.Err() != nil {
		return profile, ctx.Err()
	}

	departmentRaw, departmentErr := c.run(ctx, "contact", "+search-user", "--user-ids", userID, "--as", "user", "--format", "json")
	if departmentErr == nil {
		if resolved, decodeErr := profileFromDirectorySearch(departmentRaw, userID); decodeErr == nil {
			profile.Department = resolved.Department
			if resolved.Name != "" {
				profile.Name = resolved.Name
			}
		}
	} else if ctx.Err() != nil {
		return profile, ctx.Err()
	}

	if c.canReadBase(ctx) {
		baseRaw, baseErr := c.run(ctx, "api", "GET", "/open-apis/contact/v3/users/"+userID, "--as", "user",
			"--params", `{"user_id_type":"open_id","department_id_type":"open_department_id"}`, "--format", "json")
		if baseErr == nil {
			profile.Base, _ = baseFromDirectoryUser(baseRaw, userID)
		} else if ctx.Err() != nil {
			return profile, ctx.Err()
		}
	}
	return profile, nil
}

func avatarFromUserSearch(raw []byte, userID string) (string, error) {
	var envelope userSearchEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return "", err
	}
	for _, user := range envelope.Data.Users {
		if user.OpenID == userID {
			return user.Avatar.Avatar72, nil
		}
	}
	return "", nil
}

func profileFromDirectorySearch(raw []byte, userID string) (model.PersonProfile, error) {
	var envelope directorySearchEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return model.PersonProfile{}, err
	}
	for _, user := range envelope.Data.Users {
		if user.OpenID == userID {
			return model.PersonProfile{ID: user.OpenID, Name: user.Name, Department: strings.TrimSpace(user.Department)}, nil
		}
	}
	return model.PersonProfile{ID: userID}, nil
}

func baseFromDirectoryUser(raw []byte, userID string) (string, error) {
	var envelope directoryUserEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return "", err
	}
	if envelope.Data.User.OpenID != "" && envelope.Data.User.OpenID != userID {
		return "", nil
	}
	return strings.TrimSpace(envelope.Data.User.City), nil
}

func (c *Client) canReadBase(ctx context.Context) bool {
	c.profileScopeMu.Lock()
	defer c.profileScopeMu.Unlock()
	if time.Since(c.profileScopeChecked) < 10*time.Minute {
		return c.profileBaseReadable
	}
	status, err := c.Status(ctx)
	c.profileScopeChecked = time.Now()
	if err != nil {
		c.profileBaseReadable = false
		return false
	}
	scopes := map[string]bool{}
	for _, scope := range strings.Fields(status.Identities.User.Scope) {
		scopes[scope] = true
	}
	broad := scopes["contact:contact:readonly"] || scopes["contact:contact:access_as_app"] || scopes["contact:contact:readonly_as_app"]
	c.profileBaseReadable = (scopes["contact:contact.base:readonly"] || broad) && (scopes["contact:user.employee:readonly"] || broad)
	return c.profileBaseReadable
}

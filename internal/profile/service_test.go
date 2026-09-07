package profile

import (
	"context"
	"database/sql"
	"testing"

	"github.com/youdisn/lark-ob/internal/model"
)

type fakeRepository struct{ profile model.PersonProfile }

func (f *fakeRepository) PersonProfile(context.Context, string) (model.PersonProfile, error) {
	if f.profile.ID == "" {
		return model.PersonProfile{}, sql.ErrNoRows
	}
	return f.profile, nil
}
func (f *fakeRepository) UpsertPersonProfile(_ context.Context, profile model.PersonProfile) error {
	f.profile = profile
	return nil
}

type fakeFinder struct{ calls int }

func (f *fakeFinder) FindPersonProfile(context.Context, string, string) (model.PersonProfile, error) {
	f.calls++
	return model.PersonProfile{Avatar: "https://example/avatar.png", Base: "重庆", Department: "技术-研发"}, nil
}

func TestResolveCachesAvatar(t *testing.T) {
	repo, finder := &fakeRepository{}, &fakeFinder{}
	service := New(repo, finder)
	service.resolve(context.Background(), request{id: "ou_user", name: "用户"})
	if finder.calls != 1 || repo.profile.Avatar != "https://example/avatar.png" || repo.profile.Base != "重庆" || repo.profile.Department != "技术-研发" || repo.profile.CheckedAt == 0 {
		t.Fatalf("avatar was not cached: calls=%d profile=%#v", finder.calls, repo.profile)
	}
	service.resolve(context.Background(), request{id: "ou_user", name: "用户"})
	if finder.calls != 1 {
		t.Fatalf("fresh cache was ignored: calls=%d", finder.calls)
	}
}

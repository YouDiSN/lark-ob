package store

import (
	"context"
	"database/sql"

	"github.com/youdisn/lark-ob/internal/model"
)

func (s *Store) PersonProfile(ctx context.Context, id string) (model.PersonProfile, error) {
	var profile model.PersonProfile
	err := s.db.QueryRowContext(ctx, `SELECT id,name,avatar,base,department,checked_at FROM person_profiles WHERE id=?`, id).
		Scan(&profile.ID, &profile.Name, &profile.Avatar, &profile.Base, &profile.Department, &profile.CheckedAt)
	return profile, err
}

func (s *Store) UpsertPersonProfile(ctx context.Context, profile model.PersonProfile) error {
	if profile.ID == "" {
		return sql.ErrNoRows
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO person_profiles(id,name,avatar,base,department,checked_at) VALUES(?,?,?,?,?,?)
	 ON CONFLICT(id) DO UPDATE SET
	 name=CASE WHEN excluded.name<>'' THEN excluded.name ELSE person_profiles.name END,
	 avatar=CASE WHEN excluded.avatar<>'' THEN excluded.avatar ELSE person_profiles.avatar END,
	 base=CASE WHEN excluded.base<>'' THEN excluded.base ELSE person_profiles.base END,
	 department=CASE WHEN excluded.department<>'' THEN excluded.department ELSE person_profiles.department END,
	 checked_at=excluded.checked_at`,
		profile.ID, profile.Name, profile.Avatar, profile.Base, profile.Department, profile.CheckedAt)
	return err
}

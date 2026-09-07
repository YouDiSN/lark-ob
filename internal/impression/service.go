package impression

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/youdisn/lark-ob/internal/model"
)

type Repository interface {
	PeopleForImpression(context.Context, int64, int64) ([]Person, error)
	SelfMessageStats(context.Context, int64, int64) (int, int64, error)
	SavePersonImpression(context.Context, PersonImpression) (PersonImpression, error)
	SaveStyleProfile(context.Context, StyleProfile) (StyleProfile, error)
	GetPersonImpression(context.Context, string) (PersonImpression, error)
	GetStyleProfile(context.Context, string, string) (StyleProfile, error)
	PersonForChat(context.Context, string) (Person, error)
	PersonForImpression(context.Context, string, int64) (Person, error)
	PersonProfile(context.Context, string) (model.PersonProfile, error)
}

type Generator interface {
	GeneratePersonProfile(context.Context, Person, time.Time, time.Time) (PersonImpression, *StyleProfile, error)
	GenerateSelfStyle(context.Context, time.Time, time.Time) (StyleProfile, error)
}

type Service struct {
	repo      Repository
	generator Generator
	workers   int
}

func New(repo Repository, generator Generator, workers int) *Service {
	if workers <= 0 {
		workers = 2
	}
	return &Service{repo: repo, generator: generator, workers: workers}
}

// Update refreshes only people with source messages newer than changedSince.
// Each generated snapshot is saved atomically; a failed generation never
// replaces the last known-good impression.
func (s *Service) Update(ctx context.Context, windowStart, changedSince time.Time) (UpdateResult, error) {
	if s.generator == nil {
		return UpdateResult{}, fmt.Errorf("人物印象生成器未启用")
	}
	people, err := s.repo.PeopleForImpression(ctx, windowStart.UnixMilli(), changedSince.UnixMilli())
	if err != nil {
		return UpdateResult{}, err
	}
	result := UpdateResult{PeopleFound: len(people)}
	type generated struct {
		impression PersonImpression
		style      *StyleProfile
		err        error
	}
	jobs := make(chan Person)
	results := make(chan generated, len(people))
	var group sync.WaitGroup
	for worker := 0; worker < s.workers; worker++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for person := range jobs {
				if ctx.Err() != nil {
					return
				}
				profile, style, generateErr := s.generator.GeneratePersonProfile(ctx, person, windowStart, changedSince)
				results <- generated{impression: profile, style: style, err: generateErr}
			}
		}()
	}
	go func() {
		defer close(results)
		for _, person := range people {
			select {
			case jobs <- person:
			case <-ctx.Done():
				close(jobs)
				group.Wait()
				return
			}
		}
		close(jobs)
		group.Wait()
	}()
	for item := range results {
		if item.err != nil {
			result.Errors = append(result.Errors, item.err.Error())
			continue
		}
		if item.impression.PersonID == "" {
			result.Skipped++
			continue
		}
		if _, err := s.repo.SavePersonImpression(ctx, item.impression); err != nil {
			result.Errors = append(result.Errors, err.Error())
			continue
		}
		result.ImpressionsSaved++
		if item.style != nil {
			if _, err := s.repo.SaveStyleProfile(ctx, *item.style); err != nil {
				result.Errors = append(result.Errors, err.Error())
			} else {
				result.StylesSaved++
			}
		}
	}

	selfCount, selfLastAt, err := s.repo.SelfMessageStats(ctx, windowStart.UnixMilli(), changedSince.UnixMilli())
	if err != nil {
		result.Errors = append(result.Errors, err.Error())
	} else if selfCount >= 30 && selfLastAt > changedSince.UnixMilli() {
		style, generateErr := s.generator.GenerateSelfStyle(ctx, windowStart, changedSince)
		if generateErr != nil {
			result.Errors = append(result.Errors, generateErr.Error())
		} else if _, saveErr := s.repo.SaveStyleProfile(ctx, style); saveErr != nil {
			result.Errors = append(result.Errors, saveErr.Error())
		} else {
			result.StylesSaved++
		}
	}
	if len(result.Errors) > 0 {
		return result, fmt.Errorf("画像更新有 %d 个错误", len(result.Errors))
	}
	return result, nil
}

func (s *Service) Bundle(ctx context.Context, chatID, personID string) (Bundle, error) {
	person := Person{ID: personID}
	var err error
	if person.ID == "" {
		person, err = s.repo.PersonForChat(ctx, chatID)
		if err != nil {
			// Group chats legitimately have no implicit target.
			person = Person{}
		}
	}
	bundle := Bundle{PersonID: person.ID, PersonName: person.Name}
	if profile, profileErr := s.repo.GetStyleProfile(ctx, ScopeSelf, "me"); profileErr == nil {
		bundle.SelfStyle = &profile
	}
	if person.ID == "" {
		return bundle, nil
	}
	if directory, directoryErr := s.repo.PersonProfile(ctx, person.ID); directoryErr == nil {
		bundle.DirectoryProfile = &directory
	}
	if value, valueErr := s.repo.GetPersonImpression(ctx, person.ID); valueErr == nil {
		bundle.Impression = &value
		bundle.PersonName = value.PersonName
	}
	if value, valueErr := s.repo.GetStyleProfile(ctx, ScopeRelationship, person.ID); valueErr == nil {
		bundle.RelationshipStyle = &value
	}
	return bundle, nil
}

// RefreshPerson supports an explicit user action without changing the daily
// checkpoint. The scheduled task will still consider all source changes since
// its own last successful run.
func (s *Service) RefreshPerson(ctx context.Context, chatID, personID string, lookbackDays int) (Bundle, error) {
	if lookbackDays <= 0 {
		lookbackDays = 30
	}
	windowStart := time.Now().AddDate(0, 0, -lookbackDays)
	var person Person
	var err error
	if personID == "" {
		person, err = s.repo.PersonForChat(ctx, chatID)
	} else {
		person, err = s.repo.PersonForImpression(ctx, personID, windowStart.UnixMilli())
	}
	if err != nil {
		return Bundle{}, fmt.Errorf("找不到可生成印象的人物: %w", err)
	}
	profile, relationship, err := s.generator.GeneratePersonProfile(ctx, person, windowStart, time.Now())
	if err != nil {
		return Bundle{}, err
	}
	if profile.PersonID == "" {
		return Bundle{}, fmt.Errorf("%s 的有效消息样本不足，至少需要 3 条", person.Name)
	}
	saved, err := s.repo.SavePersonImpression(ctx, profile)
	if err != nil {
		return Bundle{}, err
	}
	bundle := Bundle{PersonID: person.ID, PersonName: person.Name, Impression: &saved}
	if directory, directoryErr := s.repo.PersonProfile(ctx, person.ID); directoryErr == nil {
		bundle.DirectoryProfile = &directory
	}
	if selfStyle, selfErr := s.repo.GetStyleProfile(ctx, ScopeSelf, "me"); selfErr == nil {
		bundle.SelfStyle = &selfStyle
	}
	if relationship != nil {
		savedStyle, saveErr := s.repo.SaveStyleProfile(ctx, *relationship)
		if saveErr != nil {
			return Bundle{}, saveErr
		}
		bundle.RelationshipStyle = &savedStyle
	}
	return bundle, nil
}

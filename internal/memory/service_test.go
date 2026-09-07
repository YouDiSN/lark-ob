package memory

import (
	"math"
	"testing"
	"time"
)

func TestEffectiveScoreUsesEvidenceHalfLife(t *testing.T) {
	now := time.Date(2026, 9, 4, 0, 0, 0, 0, time.UTC)
	claim := Claim{Confidence: .8, LastEvidenceAt: now.AddDate(0, 0, -90).UnixMilli()}
	if got := EffectiveScore(claim, now, 90); math.Abs(got-.4) > .0001 {
		t.Fatalf("score after one half-life = %f, want .4", got)
	}
}

func TestEffectiveScoreFallsBackToUpdatedAtAndClampsFuture(t *testing.T) {
	now := time.Date(2026, 9, 4, 0, 0, 0, 0, time.UTC)
	claim := Claim{Confidence: .7, UpdatedAt: now.Add(time.Hour).UnixMilli()}
	if got := EffectiveScore(claim, now, 90); got != .7 {
		t.Fatalf("future score = %f, want .7", got)
	}
}

func TestManualMemoryDoesNotDecayAndValidityControlsStatus(t *testing.T) {
	now := time.Date(2026, 9, 4, 0, 0, 0, 0, time.UTC)
	claim := Claim{SourceType: SourceManual, Importance: ImportanceImportant, UpdatedAt: now.AddDate(-2, 0, 0).UnixMilli()}
	if got := EffectiveScore(claim, now, 90); got != .97 {
		t.Fatalf("manual score = %f, want .97", got)
	}
	claim.ValidFrom = now.Add(time.Hour).UnixMilli()
	if status := ClaimStatus(claim, now); status != "scheduled" {
		t.Fatalf("status = %s, want scheduled", status)
	}
	if got := EffectiveScore(claim, now, 90); got != 0 {
		t.Fatalf("scheduled score = %f, want 0", got)
	}
	claim.ValidFrom = 0
	claim.ValidUntil = now.UnixMilli()
	if status := ClaimStatus(claim, now); status != "expired" {
		t.Fatalf("status = %s, want expired", status)
	}
}

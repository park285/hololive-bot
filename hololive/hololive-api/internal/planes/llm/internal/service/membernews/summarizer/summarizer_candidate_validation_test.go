package summarizer_test

import (
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/park285/shared-go/v2/pkg/llm/openaipreset"

	"github.com/kapu/hololive-api/internal/planes/llm/internal/service/membernews"
	"github.com/kapu/hololive-api/internal/planes/llm/internal/service/membernews/filter"
	"github.com/kapu/hololive-api/internal/planes/llm/internal/service/membernews/model"
	"github.com/kapu/hololive-api/internal/planes/llm/internal/service/membernews/summarizer"
)

type candidateResponseLLM struct{ response string }

const (
	validationMiko       = "사쿠라 미코"
	validationSuisei     = "호시마치 스이세이"
	validationOther      = "시라카미 후부키"
	validationMikoURL    = "https://hololive.hololivepro.com/news/miko"
	validationSuiseiURL  = "https://hololive.hololivepro.com/news/suisei"
	validationCollabURL  = "https://hololive.hololivepro.com/news/collab"
	validationUnknownURL = "https://hololive.hololivepro.com/news/fabricated"
)

type candidateValidationCase struct {
	name   string
	member string
	url    string
	valid  bool
}

func (c candidateResponseLLM) GenerateJSON(context.Context, openaipreset.PromptLayers, map[string]any) (string, error) {
	return c.response, nil
}

func candidateValidationCases() []candidateValidationCase {
	return []candidateValidationCase{
		{name: "matching candidate", member: validationMiko, url: validationMikoURL, valid: true},
		{name: "fabricated official URL", member: validationMiko, url: validationUnknownURL},
		{name: "modified URL query", member: validationMiko, url: validationMikoURL + "?event=fabricated"},
		{name: "URL whitespace changes exact copy", member: validationMiko, url: " " + validationMikoURL},
		{name: "member belongs to another candidate", member: validationSuisei, url: validationMikoURL},
		{name: "member is not subscribed", member: validationOther, url: validationMikoURL},
		{name: "collaboration", member: validationMiko + ", " + validationSuisei, url: validationCollabURL, valid: true},
		{name: "collaboration reordered", member: validationSuisei + ", " + validationMiko, url: validationCollabURL, valid: true},
		{name: "collaboration member subset", member: validationSuisei, url: validationCollabURL, valid: true},
		{name: "collaboration with unrelated member", member: validationMiko + ", " + validationOther, url: validationCollabURL},
		{name: "collaboration with empty member", member: validationMiko + ", ", url: validationCollabURL},
	}
}

func candidateValidationInput() *model.SummarizeInput {
	return &model.SummarizeInput{
		Period:      model.PeriodWeekly,
		Now:         time.Date(2026, time.October, 2, 12, 0, 0, 0, time.UTC),
		RoomMembers: []string{validationMiko, validationSuisei},
		Candidates: []model.FilteredCandidate{
			{MemberText: validationMiko, MatchedMembers: []string{validationMiko}, SourceURL: validationMikoURL, SourceTier: model.SourceTierOfficial},
			{MemberText: validationSuisei, MatchedMembers: []string{validationSuisei}, SourceURL: validationSuiseiURL, SourceTier: model.SourceTierOfficial},
			{MemberText: validationMiko + ", " + validationSuisei, MatchedMembers: []string{validationMiko, validationSuisei}, SourceURL: validationCollabURL, SourceTier: model.SourceTierOfficial},
		},
	}
}

func newCandidateValidationSummarizer(t *testing.T, item model.SummaryItem) *summarizer.SummarizerImpl {
	t.Helper()

	logger := slog.New(slog.DiscardHandler)

	validator, err := membernews.NewSourceValidator(t.Context(), "", nil, logger)
	if err != nil {
		t.Fatal(err)
	}

	response, err := jsonv2.Marshal(model.Digest{Period: model.PeriodWeekly, Headline: "뉴스", TopItems: []model.SummaryItem{item}})
	if err != nil {
		t.Fatal(err)
	}

	return summarizer.NewSummarizer(candidateResponseLLM{response: string(response)}, nil, validator, logger)
}

func TestSummarizerValidatesCandidateSourceAndMemberTogether(t *testing.T) {
	t.Parallel()

	for _, tc := range candidateValidationCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			item := model.SummaryItem{Member: tc.member, Category: "event", Title: "뉴스", DateText: "10/2(금)", Summary: "공식 일정", SourceURL: tc.url}
			s := newCandidateValidationSummarizer(t, item)
			digest, err := s.Summarize(t.Context(), candidateValidationInput())

			if !tc.valid {
				if digest != nil || !errors.Is(err, summarizer.ErrNoValidatedItems) {
					t.Fatalf("Summarize() = (%v, %v), want rejected output", digest, err)
				}

				return
			}

			if err != nil {
				t.Fatal(err)
			}

			if len(digest.TopItems) != 1 || digest.TopItems[0] != item {
				t.Fatalf("valid output changed: %v", digest.TopItems)
			}
		})
	}
}

func TestSummarizerPreservesCommaInCanonicalMemberNames(t *testing.T) {
	t.Parallel()

	const commaMember = "Synthetic, Member"

	input := candidateValidationInput()

	input.RoomMembers = []string{commaMember, validationMiko}

	validator, err := membernews.NewSourceValidator(t.Context(), "", nil, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}

	input.Candidates, err = filter.FilterCandidates(t.Context(), []model.Candidate{{
		Title: "Synthetic event", Members: input.RoomMembers, SourceURL: validationMikoURL,
		EventStartDate: new(input.Now),
	}}, input.Period, input.Now, input.RoomMembers, nil, validator)
	if err != nil || len(input.Candidates) != 1 || len(input.Candidates[0].MatchedMembers) != 2 {
		t.Fatalf("unexpected production-filtered candidates: %+v", input.Candidates)
	}

	for _, tc := range []struct {
		name, member string
		valid        bool
	}{
		{name: "full member name", member: commaMember, valid: true},
		{name: "name prefix is not a member", member: "Synthetic"},
		{name: "name suffix is not a member", member: "Member"},
		{name: "member name internal whitespace is literal", member: "Synthetic,Member"},
		{name: "collaboration", member: commaMember + ", " + validationMiko, valid: true},
		{name: "collaboration reordered", member: validationMiko + ", " + commaMember, valid: true},
		{name: "collaboration separator whitespace", member: validationMiko + " ,  " + commaMember, valid: true},
		{name: "collaboration subset", member: validationMiko, valid: true},
		{name: "collaboration with name fragment", member: validationMiko + ", Synthetic"},
		{name: "trailing empty member", member: commaMember + ", "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			item := model.SummaryItem{Member: tc.member, Category: "event", Title: "뉴스", DateText: "10/2(금)", Summary: "공식 일정", SourceURL: validationMikoURL}
			s := newCandidateValidationSummarizer(t, item)
			digest, summarizeErr := s.Summarize(t.Context(), input)

			if tc.valid {
				if summarizeErr != nil || digest == nil || len(digest.TopItems) != 1 || digest.TopItems[0] != item {
					t.Fatalf("valid member attribution changed: digest=%+v error=%v", digest, summarizeErr)
				}
			} else if digest != nil || !errors.Is(summarizeErr, summarizer.ErrNoValidatedItems) {
				t.Fatalf("unrelated member name fragment accepted: digest=%+v error=%v", digest, summarizeErr)
			}
		})
	}
}

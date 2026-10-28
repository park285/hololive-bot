// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package config

import "testing"

func TestLoadLLMConfigMemberNewsModel(t *testing.T) {
	t.Run("configured model", func(t *testing.T) {
		t.Setenv("MEMBER_NEWS_LLM_MODEL", "new-model")

		llm, err := LoadLLMConfig()
		if err != nil {
			t.Fatalf("LoadLLMConfig() error = %v", err)
		}

		if llm.MemberNewsModel != "new-model" {
			t.Errorf("MemberNewsModel = %q, want %q", llm.MemberNewsModel, "new-model")
		}
	})

	t.Run("defaults", func(t *testing.T) {
		t.Setenv("MEMBER_NEWS_LLM_MODEL", "")
		t.Setenv("MEMBER_NEWS_TEMPERATURE", "")

		llm, err := LoadLLMConfig()
		if err != nil {
			t.Fatalf("LoadLLMConfig() error = %v", err)
		}

		if llm.MemberNewsModel != "" || llm.MemberNewsTemperature != 0.0 {
			t.Errorf("LLM = (%q,%v), want empty model and 0 temperature", llm.MemberNewsModel, llm.MemberNewsTemperature)
		}
	})
}

func TestLoadLLMConfig_ConsensusDefaults(t *testing.T) {
	llm, err := LoadLLMConfig()
	if err != nil {
		t.Fatalf("LoadLLMConfig() error = %v", err)
	}

	if llm.MemberNews.Enabled {
		t.Error("ConsensusEnabled should default to false")
	}

	if llm.MemberNews.Confidence != 0.85 {
		t.Errorf("ConsensusConfidence = %v, want 0.85", llm.MemberNews.Confidence)
	}

	if llm.MemberNews.ReviewTimeout != 30 {
		t.Errorf("ConsensusReviewTimeout = %d, want 30", llm.MemberNews.ReviewTimeout)
	}

	if llm.MemberNews.AdjudicateTimeout != 45 {
		t.Errorf("ConsensusAdjudicateTimeout = %d, want 45", llm.MemberNews.AdjudicateTimeout)
	}

	if llm.MemberNews.ReviewerModel != "" {
		t.Errorf("ConsensusReviewerModel = %q, want empty", llm.MemberNews.ReviewerModel)
	}

	if llm.MemberNews.AdjudicatorModel != "" {
		t.Errorf("ConsensusAdjudicatorModel = %q, want empty", llm.MemberNews.AdjudicatorModel)
	}
}

func TestLoadLLMConfig_ConsensusConfidenceClamp(t *testing.T) {
	t.Run("negative clamped to 0", func(t *testing.T) {
		t.Setenv("MEMBER_NEWS_CONSENSUS_CONFIDENCE", "-0.5")

		llm, err := LoadLLMConfig()
		if err != nil {
			t.Fatalf("LoadLLMConfig() error = %v", err)
		}

		if llm.MemberNews.Confidence != 0.0 {
			t.Errorf("ConsensusConfidence = %v, want 0.0", llm.MemberNews.Confidence)
		}
	})

	t.Run("above 1 clamped to 1", func(t *testing.T) {
		t.Setenv("MEMBER_NEWS_CONSENSUS_CONFIDENCE", "1.5")

		llm, err := LoadLLMConfig()
		if err != nil {
			t.Fatalf("LoadLLMConfig() error = %v", err)
		}

		if llm.MemberNews.Confidence != 1.0 {
			t.Errorf("ConsensusConfidence = %v, want 1.0", llm.MemberNews.Confidence)
		}
	})

	t.Run("NaN falls back to default", func(t *testing.T) {
		t.Setenv("MEMBER_NEWS_CONSENSUS_CONFIDENCE", "NaN")

		llm, err := LoadLLMConfig()
		if err != nil {
			t.Fatalf("LoadLLMConfig() error = %v", err)
		}

		if llm.MemberNews.Confidence != 0.85 {
			t.Errorf("ConsensusConfidence = %v, want 0.85 (default)", llm.MemberNews.Confidence)
		}
	})

	t.Run("Inf falls back to default", func(t *testing.T) {
		t.Setenv("MEMBER_NEWS_CONSENSUS_CONFIDENCE", "Inf")

		llm, err := LoadLLMConfig()
		if err != nil {
			t.Fatalf("LoadLLMConfig() error = %v", err)
		}

		if llm.MemberNews.Confidence != 0.85 {
			t.Errorf("ConsensusConfidence = %v, want 0.85 (default)", llm.MemberNews.Confidence)
		}
	})
}

func TestLoadLLMConfig_ConsensusTimeoutMinimum(t *testing.T) {
	t.Run("review timeout below minimum", func(t *testing.T) {
		t.Setenv("MEMBER_NEWS_REVIEW_TIMEOUT_SEC", "2")

		llm, err := LoadLLMConfig()
		if err != nil {
			t.Fatalf("LoadLLMConfig() error = %v", err)
		}

		if llm.MemberNews.ReviewTimeout != 30 {
			t.Errorf("ConsensusReviewTimeout = %d, want 30 (default on <5)", llm.MemberNews.ReviewTimeout)
		}
	})

	t.Run("adjudicate timeout below minimum", func(t *testing.T) {
		t.Setenv("MEMBER_NEWS_ADJUDICATE_TIMEOUT_SEC", "3")

		llm, err := LoadLLMConfig()
		if err != nil {
			t.Fatalf("LoadLLMConfig() error = %v", err)
		}

		if llm.MemberNews.AdjudicateTimeout != 45 {
			t.Errorf("ConsensusAdjudicateTimeout = %d, want 45 (default on <5)", llm.MemberNews.AdjudicateTimeout)
		}
	})
}

func TestLoadLLMConfig_ConsensusModelFallback(t *testing.T) {
	t.Run("empty reviewer model falls back to MemberNewsModel", func(t *testing.T) {
		t.Setenv("MEMBER_NEWS_LLM_MODEL", "primary-model")

		llm, err := LoadLLMConfig()
		if err != nil {
			t.Fatalf("LoadLLMConfig() error = %v", err)
		}

		if llm.MemberNews.ReviewerModel != "" {
			t.Errorf("ConsensusReviewerModel = %q, want empty (fallback at provider level)", llm.MemberNews.ReviewerModel)
		}
	})

	t.Run("explicit reviewer model preserved", func(t *testing.T) {
		t.Setenv("MEMBER_NEWS_REVIEWER_MODEL", "gpt-4.1-mini")

		llm, err := LoadLLMConfig()
		if err != nil {
			t.Fatalf("LoadLLMConfig() error = %v", err)
		}

		if llm.MemberNews.ReviewerModel != "gpt-4.1-mini" {
			t.Errorf("ConsensusReviewerModel = %q, want gpt-4.1-mini", llm.MemberNews.ReviewerModel)
		}
	})
}

package subtitle

import (
	"context"
	"fmt"
	"time"

	"github.com/ppxb/miyabi/internal/codeid"
	"github.com/ppxb/miyabi/internal/domain"
	subtitlemeta "github.com/ppxb/miyabi/internal/domain/subtitle"
)

type TestRequest struct {
	Code   string  `json:"code" binding:"required,max=128"`
	Config *Config `json:"config"`
}

type TestResult struct {
	Code       string                `json:"code"`
	Status     string                `json:"status"`
	Candidates int                   `json:"candidates"`
	Source     string                `json:"source,omitempty"`
	Name       string                `json:"name,omitempty"`
	Language   subtitlemeta.Language `json:"language,omitempty"`
	Format     string                `json:"format,omitempty"`
	Bytes      int                   `json:"bytes,omitempty"`
	ElapsedMS  int64                 `json:"elapsed_ms"`
	Errors     []string              `json:"errors,omitempty"`
}

// TestDownload checks a real subtitle in memory, independently of automatic
// exports. It uses a settings snapshot and never changes the saved config.
func (s *Service) TestDownload(ctx context.Context, request TestRequest) (TestResult, error) {
	code, ok := codeid.Parse(request.Code)
	if !ok {
		return TestResult{}, domain.E(domain.KindInvalid, "请输入有效的影片番号", nil)
	}
	var config Config
	if request.Config == nil {
		var err error
		config, err = s.Config(ctx)
		if err != nil {
			return TestResult{}, err
		}
	} else {
		config = *request.Config
	}
	if err := config.Validate(); err != nil {
		return TestResult{}, err
	}
	if !config.Sources.Any() {
		return TestResult{}, domain.E(domain.KindInvalid, "请至少启用一个字幕源后再测试", nil)
	}
	config.Enabled = true // Manual tests remain available when auto-download is off.
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	started := time.Now()
	result := TestResult{Code: code, Status: "not_found"}
	candidates, err := s.finder.SearchWithConfig(ctx, code, subtitlemeta.IsUncensored(request.Code), config)
	result.Candidates = len(candidates)
	if err != nil {
		result.Errors = append(result.Errors, err.Error())
	}
	for i, candidate := range candidates {
		if i == maxDownloads || ctx.Err() != nil {
			break
		}
		body, language, err := s.finder.Download(ctx, candidate)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", candidate.Provider, err))
			continue
		}
		result.Status = "downloaded"
		result.Source, result.Name = candidate.Provider, candidate.Name
		result.Language, result.Format, result.Bytes = language, candidate.Format, len(body)
		break
	}
	if result.Status != "downloaded" {
		if err := ctx.Err(); err != nil {
			result.Errors = append(result.Errors, err.Error())
		}
		if len(result.Errors) > 0 || len(candidates) > 0 {
			result.Status = "failed"
		}
	}
	result.ElapsedMS = time.Since(started).Milliseconds()
	return result, nil
}

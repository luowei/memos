package v1

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/pkg/errors"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/usememos/memos/store"
)

const (
	defaultSecondBrainSyncBaseURL = "https://wiki.markdev.work"
	secondBrainSyncSourceSite     = "memos"
)

type secondBrainSyncTarget string

const (
	secondBrainSyncTargetPublic  secondBrainSyncTarget = "public"
	secondBrainSyncTargetMembers secondBrainSyncTarget = "members"
)

type syncMemoToSecondBrainRequest struct {
	Target string `json:"target"`
}

type syncMemoToSecondBrainResponse struct {
	MemoID           string `json:"memoId"`
	Slug             string `json:"slug,omitempty"`
	Visibility       string `json:"visibility,omitempty"`
	URL              string `json:"url,omitempty"`
	PublicSiteDeploy *struct {
		Queued   bool   `json:"queued,omitempty"`
		Workflow string `json:"workflow,omitempty"`
		Skipped  bool   `json:"skipped,omitempty"`
		Reason   string `json:"reason,omitempty"`
	} `json:"publicSiteDeploy,omitempty"`
}

type secondBrainSyncConfig struct {
	BaseURL      string
	SharedSecret string
}

type secondBrainSyncPostPayload struct {
	MemoID      string   `json:"memo_id"`
	Slug        string   `json:"slug"`
	Title       string   `json:"title"`
	Summary     string   `json:"summary"`
	BodyMD      string   `json:"body_md"`
	Target      string   `json:"target"`
	Categories  []string `json:"categories"`
	Tags        []string `json:"tags"`
	PublishedAt int64    `json:"published_at"`
	SourcePath  string   `json:"source_path"`
}

type secondBrainSyncAPIResponse struct {
	OK   bool `json:"ok"`
	Data struct {
		MemoID           string `json:"memo_id"`
		Slug             string `json:"slug"`
		Visibility       string `json:"visibility"`
		PublicSiteDeploy *struct {
			Queued   bool   `json:"queued,omitempty"`
			Workflow string `json:"workflow,omitempty"`
			Skipped  bool   `json:"skipped,omitempty"`
			Reason   string `json:"reason,omitempty"`
		} `json:"public_site_deploy,omitempty"`
	} `json:"data"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func (s *APIV1Service) syncMemoToSecondBrain(ctx context.Context, memoName string, target secondBrainSyncTarget) (*syncMemoToSecondBrainResponse, error) {
	if target != secondBrainSyncTargetPublic && target != secondBrainSyncTargetMembers {
		return nil, status.Errorf(codes.InvalidArgument, "invalid second brain sync target")
	}

	user, err := s.fetchCurrentUser(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get user")
	}
	if user == nil {
		return nil, status.Errorf(codes.Unauthenticated, "user not authenticated")
	}

	memoUID, err := ExtractMemoUIDFromName(memoName)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid memo name: %v", err)
	}

	memo, err := s.Store.GetMemo(ctx, &store.FindMemo{UID: &memoUID})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get memo")
	}
	if memo == nil {
		return nil, status.Errorf(codes.NotFound, "memo not found")
	}
	if memo.ParentUID != nil {
		return nil, status.Errorf(codes.InvalidArgument, "comments are not supported for second brain sync")
	}
	if memo.CreatorID != user.ID && !isSuperUser(user) {
		return nil, status.Errorf(codes.PermissionDenied, "permission denied")
	}

	config, _, err := loadSecondBrainSyncConfig(ctx, s)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to load second brain sync config: %v", err)
	}
	if strings.TrimSpace(config.SharedSecret) == "" {
		return nil, status.Errorf(codes.FailedPrecondition, "second brain sync shared secret is required")
	}

	payload, err := s.buildSecondBrainSyncPostPayload(ctx, memo, target)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to build second brain sync payload: %v", err)
	}

	apiResponse, err := postSecondBrainSyncPayload(ctx, config, payload)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to sync memo to second brain: %v", err)
	}

	exportTs := timeNowUnix()
	if _, err := s.Store.UpsertMemoExport(ctx, &store.MemoExport{
		MemoID:   memo.ID,
		ExportTs: exportTs,
	}); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to update export timestamp: %v", err)
	}

	slug := firstNonEmpty(apiResponse.Data.Slug, payload.Slug)
	return &syncMemoToSecondBrainResponse{
		MemoID:           firstNonEmpty(apiResponse.Data.MemoID, payload.MemoID),
		Slug:             slug,
		Visibility:       firstNonEmpty(apiResponse.Data.Visibility, payload.Target),
		URL:              strings.TrimRight(config.BaseURL, "/") + "/posts/" + slug,
		PublicSiteDeploy: apiResponse.Data.PublicSiteDeploy,
	}, nil
}

func (s *APIV1Service) buildSecondBrainSyncPostPayload(_ context.Context, memo *store.Memo, target secondBrainSyncTarget) (*secondBrainSyncPostPayload, error) {
	tags := []string{}
	if memo.Payload != nil && len(memo.Payload.Tags) > 0 {
		tags = append(tags, memo.Payload.Tags...)
	}

	body := stripTrailingMemoTagsFromContent(memo.Content, tags)
	title := extractMemoExportTitle(memo)
	if title == "" {
		snippet, err := s.getMemoContentSnippet(body)
		if err != nil {
			return nil, errors.Wrap(err, "failed to generate fallback title")
		}
		title = buildNormalizedTitle(snippet, exportTitleSnippetLimit)
	}
	if title == "" {
		title = "memo"
	}

	summary, err := s.getMemoContentSnippetWithLimit(body, exportDescriptionSnippetLimit)
	if err != nil {
		return nil, errors.Wrap(err, "failed to generate summary")
	}

	displayTime := s.getMemoDisplayTimeForExport(memo)
	categories := []string{}
	if len(tags) > 0 {
		categories = append(categories, tags[0])
	}

	return &secondBrainSyncPostPayload{
		MemoID:      memo.UID,
		Slug:        buildSecondBrainSyncSlug(displayTime, title),
		Title:       title,
		Summary:     summary,
		BodyMD:      body,
		Target:      string(target),
		Categories:  categories,
		Tags:        tags,
		PublishedAt: displayTime.Unix(),
		SourcePath:  secondBrainSyncSourceSite + "/" + memo.UID,
	}, nil
}

func loadSecondBrainSyncConfig(ctx context.Context, service *APIV1Service) (*secondBrainSyncConfig, string, error) {
	secondBrainSetting, err := readStoredSecondBrainSyncSetting(ctx, service)
	if err != nil {
		return nil, "", err
	}
	if secondBrainSetting != nil && (strings.TrimSpace(secondBrainSetting.BaseURL) != "" || strings.TrimSpace(secondBrainSetting.SharedSecret) != "") {
		return &secondBrainSyncConfig{
			BaseURL:      strings.TrimRight(firstNonEmpty(strings.TrimSpace(secondBrainSetting.BaseURL), getSecondBrainSyncBaseURLFromEnv(), defaultSecondBrainSyncBaseURL), "/"),
			SharedSecret: strings.TrimSpace(firstNonEmpty(secondBrainSetting.SharedSecret, os.Getenv("MEMOS_SECOND_BRAIN_SYNC_SHARED_SECRET"))),
		}, "stored", nil
	}

	stored, err := readStoredGitHubSyncSetting(ctx, service)
	if err != nil {
		return nil, "", err
	}
	if stored != nil && (strings.TrimSpace(stored.SecondBrainBaseURL) != "" || strings.TrimSpace(stored.SecondBrainSharedSecret) != "") {
		return &secondBrainSyncConfig{
			BaseURL:      strings.TrimRight(firstNonEmpty(strings.TrimSpace(stored.SecondBrainBaseURL), getSecondBrainSyncBaseURLFromEnv(), defaultSecondBrainSyncBaseURL), "/"),
			SharedSecret: strings.TrimSpace(firstNonEmpty(stored.SecondBrainSharedSecret, os.Getenv("MEMOS_SECOND_BRAIN_SYNC_SHARED_SECRET"))),
		}, "stored", nil
	}

	return &secondBrainSyncConfig{
		BaseURL:      strings.TrimRight(firstNonEmpty(getSecondBrainSyncBaseURLFromEnv(), defaultSecondBrainSyncBaseURL), "/"),
		SharedSecret: strings.TrimSpace(os.Getenv("MEMOS_SECOND_BRAIN_SYNC_SHARED_SECRET")),
	}, "env", nil
}

func getSecondBrainSyncBaseURLFromEnv() string {
	return strings.TrimRight(strings.TrimSpace(os.Getenv("MEMOS_SECOND_BRAIN_SYNC_BASE_URL")), "/")
}

func postSecondBrainSyncPayload(ctx context.Context, config *secondBrainSyncConfig, payload *secondBrainSyncPostPayload) (*secondBrainSyncAPIResponse, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, errors.Wrap(err, "failed to marshal second brain sync payload")
	}

	endpoint := strings.TrimRight(config.BaseURL, "/") + "/api/automation/memos/sync-post"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, errors.Wrap(err, "failed to create second brain sync request")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-memos-sync-secret", config.SharedSecret)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, errors.Wrap(err, "failed to execute second brain sync request")
	}
	defer resp.Body.Close()

	apiResponse := &secondBrainSyncAPIResponse{}
	if err := json.NewDecoder(resp.Body).Decode(apiResponse); err != nil {
		return nil, errors.Wrap(err, "failed to decode second brain sync response")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || !apiResponse.OK {
		message := fmt.Sprintf("second brain sync request failed with status %d", resp.StatusCode)
		if apiResponse.Error != nil && strings.TrimSpace(apiResponse.Error.Message) != "" {
			message = apiResponse.Error.Message
		}
		return nil, errors.New(message)
	}
	return apiResponse, nil
}

func buildSecondBrainSyncSlug(displayTime time.Time, title string) string {
	slug := normalizeSecondBrainSyncSlug(title)
	if slug == "" {
		slug = "memo"
	}
	return displayTime.Format("20060102") + "-" + slug
}

func normalizeSecondBrainSyncSlug(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ""
	}

	var builder strings.Builder
	lastWasSeparator := false
	for _, r := range value {
		switch {
		case unicode.IsLetter(r) || unicode.IsNumber(r):
			builder.WriteRune(r)
			lastWasSeparator = false
		case unicode.IsSpace(r) || r == '-' || r == '_':
			if !lastWasSeparator && builder.Len() > 0 {
				builder.WriteRune('-')
				lastWasSeparator = true
			}
		default:
			// Skip unsupported characters when generating the URL slug.
		}
	}

	result := strings.Trim(builder.String(), "-")
	if result == "" || !utf8.ValidString(result) {
		return ""
	}
	return result
}

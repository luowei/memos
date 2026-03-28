package v1

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/pkg/errors"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/usememos/memos/store"
)

const gitHubSyncSettingName = "CUSTOM_GITHUB_SYNC"

type gitHubSyncSetting struct {
	Token      string `json:"token,omitempty"`
	Owner      string `json:"owner,omitempty"`
	Repo       string `json:"repo,omitempty"`
	Branch     string `json:"branch,omitempty"`
	APIBaseURL string `json:"apiBaseUrl,omitempty"`
}

type gitHubSyncSettingResponse struct {
	HasToken   bool   `json:"hasToken"`
	Owner      string `json:"owner"`
	Repo       string `json:"repo"`
	Branch     string `json:"branch"`
	APIBaseURL string `json:"apiBaseUrl"`
	TokenHint  string `json:"tokenHint,omitempty"`
}

type updateGitHubSyncSettingRequest struct {
	Token      string `json:"token"`
	Owner      string `json:"owner"`
	Repo       string `json:"repo"`
	Branch     string `json:"branch"`
	APIBaseURL string `json:"apiBaseUrl"`
	ClearToken bool   `json:"clearToken"`
}

func (s *APIV1Service) getGitHubSyncSetting(ctx context.Context) (*gitHubSyncSettingResponse, error) {
	user, err := s.fetchCurrentUser(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get current user")
	}
	if user == nil {
		return nil, status.Errorf(codes.Unauthenticated, "user not authenticated")
	}
	if user.Role != store.RoleAdmin && !isSuperUser(user) {
		return nil, status.Errorf(codes.PermissionDenied, "permission denied")
	}

	config, source, err := loadGitHubSyncConfig(ctx, s)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to load github sync setting: %v", err)
	}
	if config == nil {
		config = &gitHubSyncConfig{
			Owner:   defaultGitHubSyncOwner,
			Repo:    defaultGitHubSyncRepo,
			Branch:  defaultGitHubSyncBranch,
			APIBase: defaultGitHubSyncAPIBase,
		}
	}

	response := &gitHubSyncSettingResponse{
		HasToken:   strings.TrimSpace(config.Token) != "",
		Owner:      config.Owner,
		Repo:       config.Repo,
		Branch:     config.Branch,
		APIBaseURL: config.APIBase,
	}
	if source == "env" && response.HasToken {
		response.TokenHint = "Using environment variable"
	}
	return response, nil
}

func (s *APIV1Service) updateGitHubSyncSetting(ctx context.Context, request *updateGitHubSyncSettingRequest) (*gitHubSyncSettingResponse, error) {
	user, err := s.fetchCurrentUser(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get current user")
	}
	if user == nil {
		return nil, status.Errorf(codes.Unauthenticated, "user not authenticated")
	}
	if user.Role != store.RoleAdmin && !isSuperUser(user) {
		return nil, status.Errorf(codes.PermissionDenied, "permission denied")
	}

	existing, err := readStoredGitHubSyncSetting(ctx, s)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to read github sync setting: %v", err)
	}
	if existing == nil {
		existing = &gitHubSyncSetting{}
	}

	token := existing.Token
	if request.ClearToken {
		token = ""
	} else if strings.TrimSpace(request.Token) != "" {
		token = strings.TrimSpace(request.Token)
	}

	setting := &gitHubSyncSetting{
		Token:      token,
		Owner:      firstNonEmpty(strings.TrimSpace(request.Owner), existing.Owner, defaultGitHubSyncOwner),
		Repo:       firstNonEmpty(strings.TrimSpace(request.Repo), existing.Repo, defaultGitHubSyncRepo),
		Branch:     firstNonEmpty(strings.TrimSpace(request.Branch), existing.Branch, defaultGitHubSyncBranch),
		APIBaseURL: firstNonEmpty(strings.TrimSpace(request.APIBaseURL), existing.APIBaseURL, defaultGitHubSyncAPIBase),
	}

	valueBytes, err := json.Marshal(setting)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to marshal github sync setting: %v", err)
	}
	if err := s.Store.UpsertCustomInstanceSetting(ctx, gitHubSyncSettingName, string(valueBytes), "GitHub sync integration setting"); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to save github sync setting: %v", err)
	}

	return &gitHubSyncSettingResponse{
		HasToken:   setting.Token != "",
		Owner:      setting.Owner,
		Repo:       setting.Repo,
		Branch:     setting.Branch,
		APIBaseURL: setting.APIBaseURL,
	}, nil
}

func readStoredGitHubSyncSetting(ctx context.Context, service *APIV1Service) (*gitHubSyncSetting, error) {
	raw, err := service.Store.GetCustomInstanceSetting(ctx, gitHubSyncSettingName)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get custom instance setting")
	}
	if raw == nil || strings.TrimSpace(raw.Value) == "" {
		return nil, nil
	}

	setting := &gitHubSyncSetting{}
	if err := json.Unmarshal([]byte(raw.Value), setting); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal github sync setting")
	}
	return setting, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

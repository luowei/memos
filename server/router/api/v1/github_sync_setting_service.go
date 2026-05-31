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

const (
	gitHubSyncSettingName      = "CUSTOM_GITHUB_SYNC"
	secondBrainSyncSettingName = "CUSTOM_SECOND_BRAIN_SYNC"
)

type gitHubSyncSetting struct {
	Token          string `json:"token,omitempty"`
	Owner          string `json:"owner,omitempty"`
	Repo           string `json:"repo,omitempty"`
	Branch         string `json:"branch,omitempty"`
	APIBaseURL     string `json:"apiBaseUrl,omitempty"`
	HideMemoAction *bool  `json:"hideMemoAction,omitempty"`
	// Deprecated: second-brain sync is stored under CUSTOM_SECOND_BRAIN_SYNC.
	SecondBrainBaseURL string `json:"secondBrainBaseUrl,omitempty"`
	// Deprecated: second-brain sync is stored under CUSTOM_SECOND_BRAIN_SYNC.
	SecondBrainSharedSecret string `json:"secondBrainSharedSecret,omitempty"`
}

type secondBrainSyncSetting struct {
	BaseURL      string `json:"secondBrainBaseUrl,omitempty"`
	SharedSecret string `json:"secondBrainSharedSecret,omitempty"`
}

type gitHubSyncSettingResponse struct {
	HasToken                    bool   `json:"hasToken"`
	Owner                       string `json:"owner"`
	Repo                        string `json:"repo"`
	Branch                      string `json:"branch"`
	APIBaseURL                  string `json:"apiBaseUrl"`
	TokenHint                   string `json:"tokenHint,omitempty"`
	HideMemoAction              bool   `json:"hideMemoAction"`
	SecondBrainBaseURL          string `json:"secondBrainBaseUrl"`
	HasSecondBrainSharedSecret  bool   `json:"hasSecondBrainSharedSecret"`
	SecondBrainSharedSecretHint string `json:"secondBrainSharedSecretHint,omitempty"`
}

type updateGitHubSyncSettingRequest struct {
	Token                        string `json:"token"`
	Owner                        string `json:"owner"`
	Repo                         string `json:"repo"`
	Branch                       string `json:"branch"`
	APIBaseURL                   string `json:"apiBaseUrl"`
	ClearToken                   bool   `json:"clearToken"`
	HideMemoAction               *bool  `json:"hideMemoAction"`
	SecondBrainBaseURL           string `json:"secondBrainBaseUrl"`
	SecondBrainSharedSecret      string `json:"secondBrainSharedSecret"`
	ClearSecondBrainSharedSecret bool   `json:"clearSecondBrainSharedSecret"`
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

	stored, err := readStoredGitHubSyncSetting(ctx, s)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to read github sync setting: %v", err)
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
	secondBrainConfig, secondBrainSource, err := loadSecondBrainSyncConfig(ctx, s)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to load second brain sync setting: %v", err)
	}

	response := &gitHubSyncSettingResponse{
		HasToken:                   strings.TrimSpace(config.Token) != "",
		Owner:                      config.Owner,
		Repo:                       config.Repo,
		Branch:                     config.Branch,
		APIBaseURL:                 config.APIBase,
		HideMemoAction:             gitHubSyncMemoActionHidden(stored),
		SecondBrainBaseURL:         secondBrainConfig.BaseURL,
		HasSecondBrainSharedSecret: strings.TrimSpace(secondBrainConfig.SharedSecret) != "",
	}
	if source == "env" && response.HasToken {
		response.TokenHint = "Using environment variable"
	}
	if secondBrainSource == "env" && response.HasSecondBrainSharedSecret {
		response.SecondBrainSharedSecretHint = "Using environment variable"
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
	existingSecondBrain, err := readStoredSecondBrainSyncSetting(ctx, s)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to read second brain sync setting: %v", err)
	}
	if existingSecondBrain == nil {
		existingSecondBrain = &secondBrainSyncSetting{
			BaseURL:      existing.SecondBrainBaseURL,
			SharedSecret: existing.SecondBrainSharedSecret,
		}
	}

	token := existing.Token
	if request.ClearToken {
		token = ""
	} else if strings.TrimSpace(request.Token) != "" {
		token = strings.TrimSpace(request.Token)
	}
	secondBrainSharedSecret := existingSecondBrain.SharedSecret
	if request.ClearSecondBrainSharedSecret {
		secondBrainSharedSecret = ""
	} else if strings.TrimSpace(request.SecondBrainSharedSecret) != "" {
		secondBrainSharedSecret = strings.TrimSpace(request.SecondBrainSharedSecret)
	}
	secondBrainSetting := &secondBrainSyncSetting{
		BaseURL:      strings.TrimRight(firstNonEmpty(strings.TrimSpace(request.SecondBrainBaseURL), existingSecondBrain.BaseURL, getSecondBrainSyncBaseURLFromEnv(), defaultSecondBrainSyncBaseURL), "/"),
		SharedSecret: secondBrainSharedSecret,
	}

	hideMemoAction := gitHubSyncMemoActionHidden(existing)
	if request.HideMemoAction != nil {
		hideMemoAction = *request.HideMemoAction
	}

	setting := &gitHubSyncSetting{
		Token:          token,
		Owner:          firstNonEmpty(strings.TrimSpace(request.Owner), existing.Owner, defaultGitHubSyncOwner),
		Repo:           firstNonEmpty(strings.TrimSpace(request.Repo), existing.Repo, defaultGitHubSyncRepo),
		Branch:         firstNonEmpty(strings.TrimSpace(request.Branch), existing.Branch, defaultGitHubSyncBranch),
		APIBaseURL:     firstNonEmpty(strings.TrimSpace(request.APIBaseURL), existing.APIBaseURL, defaultGitHubSyncAPIBase),
		HideMemoAction: &hideMemoAction,
	}

	valueBytes, err := json.Marshal(setting)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to marshal github sync setting: %v", err)
	}
	if err := s.Store.UpsertCustomInstanceSetting(ctx, gitHubSyncSettingName, string(valueBytes), "GitHub sync integration setting"); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to save github sync setting: %v", err)
	}
	secondBrainValueBytes, err := json.Marshal(secondBrainSetting)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to marshal second brain sync setting: %v", err)
	}
	if err := s.Store.UpsertCustomInstanceSetting(ctx, secondBrainSyncSettingName, string(secondBrainValueBytes), "Second Brain memo sync setting"); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to save second brain sync setting: %v", err)
	}

	return &gitHubSyncSettingResponse{
		HasToken:                   setting.Token != "",
		Owner:                      setting.Owner,
		Repo:                       setting.Repo,
		Branch:                     setting.Branch,
		APIBaseURL:                 setting.APIBaseURL,
		HideMemoAction:             gitHubSyncMemoActionHidden(setting),
		SecondBrainBaseURL:         secondBrainSetting.BaseURL,
		HasSecondBrainSharedSecret: secondBrainSetting.SharedSecret != "",
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

func readStoredSecondBrainSyncSetting(ctx context.Context, service *APIV1Service) (*secondBrainSyncSetting, error) {
	raw, err := service.Store.GetCustomInstanceSetting(ctx, secondBrainSyncSettingName)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get second brain sync setting")
	}
	if raw == nil || strings.TrimSpace(raw.Value) == "" {
		return nil, nil
	}

	setting := &secondBrainSyncSetting{}
	if err := json.Unmarshal([]byte(raw.Value), setting); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal second brain sync setting")
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

func gitHubSyncMemoActionHidden(setting *gitHubSyncSetting) bool {
	if setting == nil || setting.HideMemoAction == nil {
		return true
	}
	return *setting.HideMemoAction
}

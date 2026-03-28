package v1

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	"github.com/pkg/errors"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/usememos/memos/store"
)

const (
	defaultGitHubSyncOwner   = "luowei"
	defaultGitHubSyncRepo    = "luowei_github_io_src"
	defaultGitHubSyncBranch  = "master"
	defaultGitHubSyncAPIBase = "https://api.github.com"
	gitHubSyncPublicDir      = "_posts"
	gitHubSyncPrivateDir     = "_posts_private"
)

type syncMemoToGitHubResponse struct {
	Path      string `json:"path"`
	CommitURL string `json:"commitUrl,omitempty"`
}

type gitHubSyncConfig struct {
	Token   string
	Owner   string
	Repo    string
	Branch  string
	APIBase string
}

type gitHubContentItem struct {
	Name string `json:"name"`
	Path string `json:"path"`
	SHA  string `json:"sha"`
	Type string `json:"type"`
}

type gitHubPutContentRequest struct {
	Message string `json:"message"`
	Content string `json:"content"`
	Branch  string `json:"branch,omitempty"`
	SHA     string `json:"sha,omitempty"`
}

type gitHubDeleteContentRequest struct {
	Message string `json:"message"`
	SHA     string `json:"sha"`
	Branch  string `json:"branch,omitempty"`
}

type gitHubCommitResponse struct {
	Commit struct {
		HTMLURL string `json:"html_url"`
	} `json:"commit"`
	Content struct {
		Path string `json:"path"`
	} `json:"content"`
}

func (s *APIV1Service) syncMemoToGitHub(ctx context.Context, memoName string) (*syncMemoToGitHubResponse, error) {
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
		return nil, status.Errorf(codes.InvalidArgument, "comments are not supported for github sync")
	}
	if memo.CreatorID != user.ID && !isSuperUser(user) {
		return nil, status.Errorf(codes.PermissionDenied, "permission denied")
	}

	config, _, err := loadGitHubSyncConfig(ctx, s)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to load github sync config: %v", err)
	}
	if config == nil || strings.TrimSpace(config.Token) == "" {
		return nil, status.Errorf(codes.FailedPrecondition, "github sync token is required")
	}

	filename, markdown, err := s.buildMemoExport(ctx, memo)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to build memo export: %v", err)
	}

	dir := gitHubSyncPrivateDir
	if isMemoPublicForExport(memo) {
		dir = gitHubSyncPublicDir
	}
	targetPath := path.Join(dir, filename)

	items, err := listGitHubMemoCandidateFiles(ctx, config)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to list github memo files: %v", err)
	}

	suffix := "-" + memo.UID + ".md"
	var existingByMemo *gitHubContentItem
	var existingAtTarget *gitHubContentItem
	for _, item := range items {
		item := item
		if item.Path == targetPath {
			existingAtTarget = &item
		}
		if strings.HasSuffix(item.Name, suffix) {
			existingByMemo = &item
		}
	}

	commitMessage := fmt.Sprintf("Sync memo %s to GitHub", memo.UID)
	var commitResult *gitHubCommitResponse
	if existingByMemo != nil && existingByMemo.Path != targetPath {
		result, err := putGitHubContent(ctx, config, targetPath, markdown, commitMessage, existingAtTarget)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to upload memo to github: %v", err)
		}
		commitResult = result
		if err := deleteGitHubContent(ctx, config, existingByMemo.Path, existingByMemo.SHA, fmt.Sprintf("Rename synced memo %s", memo.UID)); err != nil {
			return nil, status.Errorf(codes.Internal, "failed to remove outdated github memo file: %v", err)
		}
	} else {
		existingTarget := existingAtTarget
		if existingTarget == nil && existingByMemo != nil {
			existingTarget = existingByMemo
		}
		result, err := putGitHubContent(ctx, config, targetPath, markdown, commitMessage, existingTarget)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to upload memo to github: %v", err)
		}
		commitResult = result
	}

	exportTs := timeNowUnix()
	if _, err := s.Store.UpsertMemoExport(ctx, &store.MemoExport{
		MemoID:   memo.ID,
		ExportTs: exportTs,
	}); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to update export timestamp: %v", err)
	}

	commitURL := fmt.Sprintf("https://github.com/%s/%s/blob/%s/%s", config.Owner, config.Repo, config.Branch, targetPath)
	if commitResult != nil && commitResult.Commit.HTMLURL != "" {
		commitURL = commitResult.Commit.HTMLURL
	}
	return &syncMemoToGitHubResponse{
		Path:      targetPath,
		CommitURL: commitURL,
	}, nil
}

func getGitHubSyncConfig() (*gitHubSyncConfig, error) {
	return getGitHubSyncConfigFromEnv()
}

func loadGitHubSyncConfig(ctx context.Context, service *APIV1Service) (*gitHubSyncConfig, string, error) {
	stored, err := readStoredGitHubSyncSetting(ctx, service)
	if err != nil {
		return nil, "", err
	}
	if stored != nil {
		return &gitHubSyncConfig{
			Token:   strings.TrimSpace(stored.Token),
			Owner:   firstNonEmpty(strings.TrimSpace(stored.Owner), defaultGitHubSyncOwner),
			Repo:    firstNonEmpty(strings.TrimSpace(stored.Repo), defaultGitHubSyncRepo),
			Branch:  firstNonEmpty(strings.TrimSpace(stored.Branch), defaultGitHubSyncBranch),
			APIBase: firstNonEmpty(strings.TrimSpace(stored.APIBaseURL), defaultGitHubSyncAPIBase),
		}, "stored", nil
	}

	config, err := getGitHubSyncConfigFromEnv()
	if err != nil {
		return nil, "", nil
	}
	return config, "env", nil
}

func getGitHubSyncConfigFromEnv() (*gitHubSyncConfig, error) {
	token := strings.TrimSpace(os.Getenv("MEMOS_GITHUB_SYNC_TOKEN"))
	if token == "" {
		return nil, errors.New("github sync token is required in MEMOS_GITHUB_SYNC_TOKEN")
	}

	owner := strings.TrimSpace(os.Getenv("MEMOS_GITHUB_SYNC_REPO_OWNER"))
	if owner == "" {
		owner = defaultGitHubSyncOwner
	}
	repo := strings.TrimSpace(os.Getenv("MEMOS_GITHUB_SYNC_REPO_NAME"))
	if repo == "" {
		repo = defaultGitHubSyncRepo
	}
	branch := strings.TrimSpace(os.Getenv("MEMOS_GITHUB_SYNC_BRANCH"))
	if branch == "" {
		branch = defaultGitHubSyncBranch
	}
	apiBase := strings.TrimSpace(os.Getenv("MEMOS_GITHUB_SYNC_API_BASE_URL"))
	if apiBase == "" {
		apiBase = defaultGitHubSyncAPIBase
	}

	return &gitHubSyncConfig{
		Token:   token,
		Owner:   owner,
		Repo:    repo,
		Branch:  branch,
		APIBase: strings.TrimRight(apiBase, "/"),
	}, nil
}

func listGitHubDirectoryContents(ctx context.Context, config *gitHubSyncConfig, dir string) ([]gitHubContentItem, error) {
	endpoint := fmt.Sprintf("%s/repos/%s/%s/contents/%s?ref=%s", config.APIBase, config.Owner, config.Repo, url.PathEscape(dir), url.QueryEscape(config.Branch))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create github list request")
	}
	req.Header.Set("Authorization", "Bearer "+config.Token)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, errors.Wrap(err, "failed to execute github list request")
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return []gitHubContentItem{}, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, errors.Errorf("github list request failed with status %d", resp.StatusCode)
	}

	items := []gitHubContentItem{}
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		return nil, errors.Wrap(err, "failed to decode github directory response")
	}
	return items, nil
}

func listGitHubMemoCandidateFiles(ctx context.Context, config *gitHubSyncConfig) ([]gitHubContentItem, error) {
	dirs := []string{gitHubSyncPublicDir, gitHubSyncPrivateDir}
	items := []gitHubContentItem{}
	for _, dir := range dirs {
		list, err := listGitHubDirectoryContents(ctx, config, dir)
		if err != nil {
			return nil, err
		}
		items = append(items, list...)
	}
	return items, nil
}

func putGitHubContent(ctx context.Context, config *gitHubSyncConfig, filePath, content, message string, existing *gitHubContentItem) (*gitHubCommitResponse, error) {
	payload := &gitHubPutContentRequest{
		Message: message,
		Content: base64.StdEncoding.EncodeToString([]byte(content)),
		Branch:  config.Branch,
	}
	if existing != nil && existing.Path == filePath {
		payload.SHA = existing.SHA
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, errors.Wrap(err, "failed to marshal github put request")
	}

	endpoint := fmt.Sprintf("%s/repos/%s/%s/contents/%s", config.APIBase, config.Owner, config.Repo, url.PathEscape(filePath))
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, errors.Wrap(err, "failed to create github put request")
	}
	req.Header.Set("Authorization", "Bearer "+config.Token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, errors.Wrap(err, "failed to execute github put request")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, errors.Errorf("github put request failed with status %d", resp.StatusCode)
	}

	result := &gitHubCommitResponse{}
	if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
		return nil, errors.Wrap(err, "failed to decode github put response")
	}
	return result, nil
}

func deleteGitHubContent(ctx context.Context, config *gitHubSyncConfig, filePath, sha, message string) error {
	payload := &gitHubDeleteContentRequest{
		Message: message,
		SHA:     sha,
		Branch:  config.Branch,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return errors.Wrap(err, "failed to marshal github delete request")
	}

	endpoint := fmt.Sprintf("%s/repos/%s/%s/contents/%s", config.APIBase, config.Owner, config.Repo, url.PathEscape(filePath))
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint, bytes.NewReader(body))
	if err != nil {
		return errors.Wrap(err, "failed to create github delete request")
	}
	req.Header.Set("Authorization", "Bearer "+config.Token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return errors.Wrap(err, "failed to execute github delete request")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return errors.Errorf("github delete request failed with status %d", resp.StatusCode)
	}
	return nil
}

func timeNowUnix() int64 {
	return time.Now().Unix()
}

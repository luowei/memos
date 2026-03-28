package v1

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/usememos/memos/internal/profile"
	"github.com/usememos/memos/plugin/markdown"
	apiv1 "github.com/usememos/memos/proto/gen/api/v1"
	"github.com/usememos/memos/server/auth"
	"github.com/usememos/memos/store"
	teststore "github.com/usememos/memos/store/test"
)

func TestSyncMemoToGitHub(t *testing.T) {
	ctx := context.Background()

	testStore := teststore.NewTestingStore(ctx, t)
	t.Cleanup(func() {
		testStore.Close()
	})

	service := &APIV1Service{
		Store: testStore,
		Profile: &profile.Profile{
			Data: t.TempDir(),
		},
		MarkdownService: markdown.NewService(
			markdown.WithTagExtension(),
		),
		SSEHub: NewSSEHub(),
	}

	user, err := testStore.CreateUser(ctx, &store.User{
		Username: "github-user",
		Email:    "github-user@example.com",
		Role:     store.RoleUser,
	})
	require.NoError(t, err)

	userCtx := context.WithValue(ctx, auth.UserIDContextKey, user.ID)

	createTime := time.Date(2026, 3, 27, 8, 0, 0, 0, time.UTC)
	memo, err := service.CreateMemo(userCtx, &apiv1.CreateMemoRequest{
		Memo: &apiv1.Memo{
			Content:    "# Hello World\n\nBody content.\n\n#jekyll #golang",
			Visibility: apiv1.Visibility_PUBLIC,
			CreateTime: timestamppb.New(createTime),
		},
	})
	require.NoError(t, err)

	memoUID := strings.TrimPrefix(memo.Name, "memos/")
	oldPath := "_posts/20260327-old_title-" + memoUID + ".md"
	newPath := "_posts/20260327-hello_world-" + memoUID + ".md"

	var putPath string
	var deletePath string
	var uploadedContent string

	githubServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))

		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.String(), "/contents/_posts"):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode([]map[string]string{
				{
					"name": "20260327-old_title-" + memoUID + ".md",
					"path": oldPath,
					"sha":  "old-sha",
					"type": "file",
				},
			})
		case r.Method == http.MethodPut && strings.Contains(r.URL.EscapedPath(), "/contents/"):
			putPath = decodeGitHubContentsPath(r.URL.EscapedPath())
			var body struct {
				Content string `json:"content"`
				SHA     string `json:"sha"`
			}
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			contentBytes, err := decodeBase64String(body.Content)
			require.NoError(t, err)
			uploadedContent = string(contentBytes)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"content": map[string]string{
					"path": newPath,
				},
				"commit": map[string]string{
					"html_url": "https://github.example.com/commit/123",
				},
			})
		case r.Method == http.MethodDelete && strings.Contains(r.URL.EscapedPath(), "/contents/"):
			deletePath = decodeGitHubContentsPath(r.URL.EscapedPath())
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer githubServer.Close()

	t.Setenv("MEMOS_GITHUB_SYNC_TOKEN", "test-token")
	t.Setenv("MEMOS_GITHUB_SYNC_REPO_OWNER", "luowei")
	t.Setenv("MEMOS_GITHUB_SYNC_REPO_NAME", "luowei_github_io_src")
	t.Setenv("MEMOS_GITHUB_SYNC_BRANCH", "master")
	t.Setenv("MEMOS_GITHUB_SYNC_API_BASE_URL", githubServer.URL)

	response, err := service.syncMemoToGitHub(userCtx, memo.Name)
	require.NoError(t, err)
	require.Equal(t, newPath, response.Path)
	require.Equal(t, newPath, putPath)
	require.Equal(t, oldPath, deletePath)
	require.NotContains(t, uploadedContent, "#jekyll #golang")
	require.Contains(t, uploadedContent, "layout: post")
	require.Contains(t, uploadedContent, "title: Hello World")
	require.NotContains(t, uploadedContent, "visibility: private\n")
	require.NotContains(t, uploadedContent, "comments: false\n")

	storeMemo, err := testStore.GetMemo(userCtx, &store.FindMemo{UID: &memoUID})
	require.NoError(t, err)
	require.NotNil(t, storeMemo)
	exports, err := testStore.ListMemoExports(userCtx, &store.FindMemoExport{MemoID: &storeMemo.ID})
	require.NoError(t, err)
	require.Len(t, exports, 1)
	require.NotZero(t, exports[0].ExportTs)
}

func TestSyncMemoToGitHub_MovesBetweenDirectoriesOnVisibilityChange(t *testing.T) {
	ctx := context.Background()

	testStore := teststore.NewTestingStore(ctx, t)
	t.Cleanup(func() {
		testStore.Close()
	})

	service := &APIV1Service{
		Store: testStore,
		Profile: &profile.Profile{
			Data: t.TempDir(),
		},
		MarkdownService: markdown.NewService(
			markdown.WithTagExtension(),
		),
		SSEHub: NewSSEHub(),
	}

	user, err := testStore.CreateUser(ctx, &store.User{
		Username: "github-user-private",
		Email:    "github-user-private@example.com",
		Role:     store.RoleUser,
	})
	require.NoError(t, err)

	userCtx := context.WithValue(ctx, auth.UserIDContextKey, user.ID)

	createTime := time.Date(2026, 3, 27, 8, 0, 0, 0, time.UTC)
	memo, err := service.CreateMemo(userCtx, &apiv1.CreateMemoRequest{
		Memo: &apiv1.Memo{
			Content:    "# Hello World\n\nBody content.",
			Visibility: apiv1.Visibility_PUBLIC,
			CreateTime: timestamppb.New(createTime),
		},
	})
	require.NoError(t, err)

	_, err = service.UpdateMemo(userCtx, &apiv1.UpdateMemoRequest{
		Memo: &apiv1.Memo{
			Name:       memo.Name,
			Visibility: apiv1.Visibility_PRIVATE,
		},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"visibility"}},
	})
	require.NoError(t, err)

	memoUID := strings.TrimPrefix(memo.Name, "memos/")
	oldPath := "_posts/20260327-hello_world-" + memoUID + ".md"
	newPath := "_posts_private/20260327-hello_world-" + memoUID + ".md"

	var putPath string
	var deletePath string
	var uploadedContent string

	githubServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))

		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.String(), "/contents/_posts?"):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode([]map[string]string{
				{
					"name": "20260327-hello_world-" + memoUID + ".md",
					"path": oldPath,
					"sha":  "old-sha",
					"type": "file",
				},
			})
		case r.Method == http.MethodGet && strings.Contains(r.URL.String(), "/contents/_posts_private?"):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode([]map[string]string{})
		case r.Method == http.MethodPut && strings.Contains(r.URL.EscapedPath(), "/contents/"):
			putPath = decodeGitHubContentsPath(r.URL.EscapedPath())
			var body struct {
				Content string `json:"content"`
			}
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			contentBytes, err := decodeBase64String(body.Content)
			require.NoError(t, err)
			uploadedContent = string(contentBytes)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"content": map[string]string{
					"path": newPath,
				},
				"commit": map[string]string{
					"html_url": "https://github.example.com/commit/456",
				},
			})
		case r.Method == http.MethodDelete && strings.Contains(r.URL.EscapedPath(), "/contents/"):
			deletePath = decodeGitHubContentsPath(r.URL.EscapedPath())
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer githubServer.Close()

	t.Setenv("MEMOS_GITHUB_SYNC_TOKEN", "test-token")
	t.Setenv("MEMOS_GITHUB_SYNC_REPO_OWNER", "luowei")
	t.Setenv("MEMOS_GITHUB_SYNC_REPO_NAME", "luowei_github_io_src")
	t.Setenv("MEMOS_GITHUB_SYNC_BRANCH", "master")
	t.Setenv("MEMOS_GITHUB_SYNC_API_BASE_URL", githubServer.URL)

	response, err := service.syncMemoToGitHub(userCtx, memo.Name)
	require.NoError(t, err)
	require.Equal(t, newPath, response.Path)
	require.Equal(t, newPath, putPath)
	require.Equal(t, oldPath, deletePath)
	require.Contains(t, uploadedContent, "visibility: private\n")
	require.Contains(t, uploadedContent, "comments: false\n")
}

func TestSyncMemoToGitHub_MovesPrivateMemoBackToPublicDirectory(t *testing.T) {
	ctx := context.Background()

	testStore := teststore.NewTestingStore(ctx, t)
	t.Cleanup(func() {
		testStore.Close()
	})

	service := &APIV1Service{
		Store: testStore,
		Profile: &profile.Profile{
			Data: t.TempDir(),
		},
		MarkdownService: markdown.NewService(
			markdown.WithTagExtension(),
		),
		SSEHub: NewSSEHub(),
	}

	user, err := testStore.CreateUser(ctx, &store.User{
		Username: "github-user-public",
		Email:    "github-user-public@example.com",
		Role:     store.RoleUser,
	})
	require.NoError(t, err)

	userCtx := context.WithValue(ctx, auth.UserIDContextKey, user.ID)

	createTime := time.Date(2026, 3, 27, 8, 0, 0, 0, time.UTC)
	memo, err := service.CreateMemo(userCtx, &apiv1.CreateMemoRequest{
		Memo: &apiv1.Memo{
			Content:    "# Hello World\n\nBody content.",
			Visibility: apiv1.Visibility_PRIVATE,
			CreateTime: timestamppb.New(createTime),
		},
	})
	require.NoError(t, err)

	_, err = service.UpdateMemo(userCtx, &apiv1.UpdateMemoRequest{
		Memo: &apiv1.Memo{
			Name:       memo.Name,
			Visibility: apiv1.Visibility_PUBLIC,
		},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"visibility"}},
	})
	require.NoError(t, err)

	memoUID := strings.TrimPrefix(memo.Name, "memos/")
	oldPath := "_posts_private/20260327-hello_world-" + memoUID + ".md"
	newPath := "_posts/20260327-hello_world-" + memoUID + ".md"

	var putPath string
	var deletePath string
	var uploadedContent string

	githubServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))

		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.String(), "/contents/_posts?"):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode([]map[string]string{})
		case r.Method == http.MethodGet && strings.Contains(r.URL.String(), "/contents/_posts_private?"):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode([]map[string]string{
				{
					"name": "20260327-hello_world-" + memoUID + ".md",
					"path": oldPath,
					"sha":  "old-sha",
					"type": "file",
				},
			})
		case r.Method == http.MethodPut && strings.Contains(r.URL.EscapedPath(), "/contents/"):
			putPath = decodeGitHubContentsPath(r.URL.EscapedPath())
			var body struct {
				Content string `json:"content"`
			}
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			contentBytes, err := decodeBase64String(body.Content)
			require.NoError(t, err)
			uploadedContent = string(contentBytes)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"content": map[string]string{
					"path": newPath,
				},
				"commit": map[string]string{
					"html_url": "https://github.example.com/commit/789",
				},
			})
		case r.Method == http.MethodDelete && strings.Contains(r.URL.EscapedPath(), "/contents/"):
			deletePath = decodeGitHubContentsPath(r.URL.EscapedPath())
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer githubServer.Close()

	t.Setenv("MEMOS_GITHUB_SYNC_TOKEN", "test-token")
	t.Setenv("MEMOS_GITHUB_SYNC_REPO_OWNER", "luowei")
	t.Setenv("MEMOS_GITHUB_SYNC_REPO_NAME", "luowei_github_io_src")
	t.Setenv("MEMOS_GITHUB_SYNC_BRANCH", "master")
	t.Setenv("MEMOS_GITHUB_SYNC_API_BASE_URL", githubServer.URL)

	response, err := service.syncMemoToGitHub(userCtx, memo.Name)
	require.NoError(t, err)
	require.Equal(t, newPath, response.Path)
	require.Equal(t, newPath, putPath)
	require.Equal(t, oldPath, deletePath)
	require.NotContains(t, uploadedContent, "visibility: private\n")
	require.NotContains(t, uploadedContent, "comments: false\n")
}

func TestSyncMemoToGitHub_MovesProtectedMemoToPrivateDirectory(t *testing.T) {
	ctx := context.Background()

	testStore := teststore.NewTestingStore(ctx, t)
	t.Cleanup(func() {
		testStore.Close()
	})

	service := &APIV1Service{
		Store: testStore,
		Profile: &profile.Profile{
			Data: t.TempDir(),
		},
		MarkdownService: markdown.NewService(
			markdown.WithTagExtension(),
		),
		SSEHub: NewSSEHub(),
	}

	user, err := testStore.CreateUser(ctx, &store.User{
		Username: "github-user-protected",
		Email:    "github-user-protected@example.com",
		Role:     store.RoleUser,
	})
	require.NoError(t, err)

	userCtx := context.WithValue(ctx, auth.UserIDContextKey, user.ID)

	createTime := time.Date(2026, 3, 27, 8, 0, 0, 0, time.UTC)
	memo, err := service.CreateMemo(userCtx, &apiv1.CreateMemoRequest{
		Memo: &apiv1.Memo{
			Content:    "# Hello World\n\nBody content.",
			Visibility: apiv1.Visibility_PUBLIC,
			CreateTime: timestamppb.New(createTime),
		},
	})
	require.NoError(t, err)

	_, err = service.UpdateMemo(userCtx, &apiv1.UpdateMemoRequest{
		Memo: &apiv1.Memo{
			Name:       memo.Name,
			Visibility: apiv1.Visibility_PROTECTED,
		},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"visibility"}},
	})
	require.NoError(t, err)

	memoUID := strings.TrimPrefix(memo.Name, "memos/")
	oldPath := "_posts/20260327-hello_world-" + memoUID + ".md"
	newPath := "_posts_private/20260327-hello_world-" + memoUID + ".md"

	var putPath string
	var deletePath string
	var uploadedContent string

	githubServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))

		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.String(), "/contents/_posts?"):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode([]map[string]string{
				{
					"name": "20260327-hello_world-" + memoUID + ".md",
					"path": oldPath,
					"sha":  "old-sha",
					"type": "file",
				},
			})
		case r.Method == http.MethodGet && strings.Contains(r.URL.String(), "/contents/_posts_private?"):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode([]map[string]string{})
		case r.Method == http.MethodPut && strings.Contains(r.URL.EscapedPath(), "/contents/"):
			putPath = decodeGitHubContentsPath(r.URL.EscapedPath())
			var body struct {
				Content string `json:"content"`
			}
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			contentBytes, err := decodeBase64String(body.Content)
			require.NoError(t, err)
			uploadedContent = string(contentBytes)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"content": map[string]string{
					"path": newPath,
				},
				"commit": map[string]string{
					"html_url": "https://github.example.com/commit/101112",
				},
			})
		case r.Method == http.MethodDelete && strings.Contains(r.URL.EscapedPath(), "/contents/"):
			deletePath = decodeGitHubContentsPath(r.URL.EscapedPath())
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer githubServer.Close()

	t.Setenv("MEMOS_GITHUB_SYNC_TOKEN", "test-token")
	t.Setenv("MEMOS_GITHUB_SYNC_REPO_OWNER", "luowei")
	t.Setenv("MEMOS_GITHUB_SYNC_REPO_NAME", "luowei_github_io_src")
	t.Setenv("MEMOS_GITHUB_SYNC_BRANCH", "master")
	t.Setenv("MEMOS_GITHUB_SYNC_API_BASE_URL", githubServer.URL)

	response, err := service.syncMemoToGitHub(userCtx, memo.Name)
	require.NoError(t, err)
	require.Equal(t, newPath, response.Path)
	require.Equal(t, newPath, putPath)
	require.Equal(t, oldPath, deletePath)
	require.Contains(t, uploadedContent, "visibility: private\n")
	require.Contains(t, uploadedContent, "comments: false\n")
}

func decodeBase64String(value string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(value)
}

func decodeGitHubContentsPath(escapedPath string) string {
	prefix := "/repos/luowei/luowei_github_io_src/contents/"
	index := strings.Index(escapedPath, prefix)
	if index == -1 {
		return escapedPath
	}
	value, err := url.PathUnescape(escapedPath[index+len(prefix):])
	if err != nil {
		return escapedPath[index+len(prefix):]
	}
	return value
}

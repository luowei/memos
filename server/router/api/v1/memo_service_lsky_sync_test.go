package v1

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/usememos/memos/internal/profile"
	"github.com/usememos/memos/plugin/markdown"
	apiv1 "github.com/usememos/memos/proto/gen/api/v1"
	"github.com/usememos/memos/server/auth"
	"github.com/usememos/memos/store"
	teststore "github.com/usememos/memos/store/test"
)

func TestSyncMemoAttachmentsToLsky(t *testing.T) {
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
		Username: "lsky-user",
		Email:    "lsky-user@example.com",
		Role:     store.RoleUser,
	})
	require.NoError(t, err)

	userCtx := context.WithValue(ctx, auth.UserIDContextKey, user.ID)

	imageMemo, err := service.CreateMemo(userCtx, &apiv1.CreateMemoRequest{
		Memo: &apiv1.Memo{
			Content:    "Memo with image attachment",
			Visibility: apiv1.Visibility_PRIVATE,
		},
	})
	require.NoError(t, err)

	_, err = service.CreateAttachment(userCtx, &apiv1.CreateAttachmentRequest{
		Attachment: &apiv1.Attachment{
			Filename: "cover.png",
			Type:     "image/png",
			Content:  []byte("fake png bytes"),
			Memo:     &imageMemo.Name,
		},
	})
	require.NoError(t, err)

	zipMemo, err := service.CreateMemo(userCtx, &apiv1.CreateMemoRequest{
		Memo: &apiv1.Memo{
			Content:    "Memo with unsupported attachment",
			Visibility: apiv1.Visibility_PRIVATE,
		},
	})
	require.NoError(t, err)

	_, err = service.CreateAttachment(userCtx, &apiv1.CreateAttachmentRequest{
		Attachment: &apiv1.Attachment{
			Filename: "archive.zip",
			Type:     "application/zip",
			Content:  []byte("zip bytes"),
			Memo:     &zipMemo.Name,
		},
	})
	require.NoError(t, err)

	var uploadedFilenames []string
	lskyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/upload", r.URL.Path)
		require.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))

		require.NoError(t, r.ParseMultipartForm(32<<20))
		file, header, err := r.FormFile("file")
		require.NoError(t, err)
		defer file.Close()

		_, err = io.ReadAll(file)
		require.NoError(t, err)
		uploadedFilenames = append(uploadedFilenames, header.Filename)

		w.Header().Set("Content-Type", "application/json")
		_, err = fmt.Fprintf(
			w,
			`{"status":true,"message":"ok","data":{"links":{"url":"https://img.example.com/%s","markdown":"![]()"}}}`,
			header.Filename,
		)
		require.NoError(t, err)
	}))
	defer lskyServer.Close()

	response, err := service.syncMemoAttachmentsToLsky(userCtx, &syncMemoAttachmentsToLskyRequest{
		BaseURL: lskyServer.URL,
		Token:   "test-token",
	})
	require.NoError(t, err)
	require.Equal(t, int32(2), response.ScannedMemos)
	require.Equal(t, int32(2), response.MemosWithAttachments)
	require.Equal(t, int32(1), response.UpdatedMemos)
	require.Equal(t, int32(1), response.SkippedMemos)
	require.Equal(t, int32(1), response.UploadedFiles)
	require.Equal(t, []string{"cover.png"}, uploadedFilenames)

	updatedImageMemo, err := service.GetMemo(userCtx, &apiv1.GetMemoRequest{Name: imageMemo.Name})
	require.NoError(t, err)
	require.Contains(t, updatedImageMemo.Content, lskySyncMarkerStart)
	require.Contains(t, updatedImageMemo.Content, "![cover.png](https://img.example.com/cover.png)")
	require.Contains(t, updatedImageMemo.Content, lskySyncMarkerEnd)

	unchangedZipMemo, err := service.GetMemo(userCtx, &apiv1.GetMemoRequest{Name: zipMemo.Name})
	require.NoError(t, err)
	require.Equal(t, "Memo with unsupported attachment", unchangedZipMemo.Content)

	require.Len(t, response.Results, 2)
	resultsByMemo := map[string]*memoLskySyncResult{}
	for _, item := range response.Results {
		resultsByMemo[item.MemoName] = item
	}
	require.Equal(t, "updated", resultsByMemo[imageMemo.Name].Status)
	require.Equal(t, int32(1), resultsByMemo[imageMemo.Name].AttachmentCount)
	require.Equal(t, "skipped", resultsByMemo[zipMemo.Name].Status)
	require.Equal(t, int32(1), resultsByMemo[zipMemo.Name].AttachmentCount)
	require.True(t, strings.Contains(resultsByMemo[zipMemo.Name].Reason, "unsupported"))
}

func TestBuildLskyMarkdownImageLine(t *testing.T) {
	line := buildLskyMarkdownImageLine("test[file].png", "https://img.example.com/test.png")
	require.Equal(t, "![test(file).png](https://img.example.com/test.png)", line)
}

func TestParseOptionalStrategyID(t *testing.T) {
	value, err := parseOptionalStrategyID("12")
	require.NoError(t, err)
	require.Equal(t, "12", value)

	value, err = parseOptionalStrategyID("")
	require.NoError(t, err)
	require.Equal(t, "", value)

	_, err = parseOptionalStrategyID("abc")
	require.Error(t, err)
}

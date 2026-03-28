package v1

import (
	"context"
	"os"
	"path/filepath"
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

func TestExportMemos(t *testing.T) {
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
		Username: "export-user",
		Email:    "export-user@example.com",
		Role:     store.RoleUser,
	})
	require.NoError(t, err)

	userCtx := context.WithValue(ctx, auth.UserIDContextKey, user.ID)

	firstCreateTime := time.Date(2026, 3, 27, 8, 0, 0, 0, time.UTC)
	firstMemo, err := service.CreateMemo(userCtx, &apiv1.CreateMemoRequest{
		Memo: &apiv1.Memo{
			Content:    "# Hello World\n\nThis memo is ready for export.\n\n#jekyll #golang",
			Visibility: apiv1.Visibility_PRIVATE,
			CreateTime: timestamppb.New(firstCreateTime),
		},
	})
	require.NoError(t, err)

	secondCreateTime := time.Date(2026, 3, 28, 9, 30, 0, 0, time.UTC)
	secondMemo, err := service.CreateMemo(userCtx, &apiv1.CreateMemoRequest{
		Memo: &apiv1.Memo{
			Content:    "Second memo for export without explicit title.\n\n#daily",
			Visibility: apiv1.Visibility_PRIVATE,
			CreateTime: timestamppb.New(secondCreateTime),
		},
	})
	require.NoError(t, err)

	_, err = service.UpdateMemo(userCtx, &apiv1.UpdateMemoRequest{
		Memo: &apiv1.Memo{
			Name:  secondMemo.Name,
			State: apiv1.State_ARCHIVED,
		},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"state"}},
	})
	require.NoError(t, err)

	response, err := service.exportMemos(userCtx, &exportMemosRequest{
		OutputDirectory: "exports/posts",
	})
	require.NoError(t, err)
	require.Equal(t, int32(2), response.ExportedCount)

	expectedDir := filepath.Join(service.Profile.Data, "exports/posts")
	require.Equal(t, expectedDir, response.OutputDirectory)

	entries, err := os.ReadDir(expectedDir)
	require.NoError(t, err)
	require.Len(t, entries, 2)

	firstUID := strings.TrimPrefix(firstMemo.Name, "memos/")
	firstFilename := filepath.Join(expectedDir, "2026-03-27-hello-world-"+firstUID+".md")
	firstContent, err := os.ReadFile(firstFilename)
	require.NoError(t, err)
	require.Contains(t, string(firstContent), "layout: post\n")
	require.Contains(t, string(firstContent), "title: Hello World\n")
	require.Contains(t, string(firstContent), "date: \"2026-03-27\"\n")
	require.Contains(t, string(firstContent), "description: 'Hello World This memo is ready for export. #jekyll #golang'\n")
	require.Contains(t, string(firstContent), "categories: jekyll\n")
	require.Contains(t, string(firstContent), "- jekyll\n")
	require.Contains(t, string(firstContent), "- golang\n")
	require.Contains(t, string(firstContent), "visibility: private\n")
	require.Contains(t, string(firstContent), "comments: false\n")
	require.Contains(t, string(firstContent), "\n# Hello World\n\nThis memo is ready for export.")

	secondUID := strings.TrimPrefix(secondMemo.Name, "memos/")
	secondFilename := filepath.Join(expectedDir, "2026-03-28-second-memo-for-"+secondUID+".md")
	secondContent, err := os.ReadFile(secondFilename)
	require.NoError(t, err)
	require.Contains(t, string(secondContent), "title: second memo for\n")
	require.Contains(t, string(secondContent), "categories: daily\n")
	require.Contains(t, string(secondContent), "- daily\n")

	firstMetadata, err := service.getMemoExportMetadata(userCtx, firstMemo.Name)
	require.NoError(t, err)
	require.NotNil(t, firstMetadata.ExportTs)
	require.GreaterOrEqual(t, *firstMetadata.ExportTs, firstCreateTime.Unix())

	secondMetadata, err := service.getMemoExportMetadata(userCtx, secondMemo.Name)
	require.NoError(t, err)
	require.NotNil(t, secondMetadata.ExportTs)

	updatedMemoTime := time.Date(2026, 3, 29, 10, 15, 0, 0, time.UTC)
	_, err = service.UpdateMemo(userCtx, &apiv1.UpdateMemoRequest{
		Memo: &apiv1.Memo{
			Name:       firstMemo.Name,
			Content:    "# Hello World\n\nThis memo is updated after export.\n\n#jekyll #golang",
			UpdateTime: timestamppb.New(updatedMemoTime),
		},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"content", "update_time"}},
	})
	require.NoError(t, err)

	exports, err := testStore.ListMemoExports(userCtx, &store.FindMemoExport{})
	require.NoError(t, err)
	require.Len(t, exports, 2)

	firstMemoUID := strings.TrimPrefix(firstMemo.Name, "memos/")
	firstStoreMemo, err := testStore.GetMemo(userCtx, &store.FindMemo{UID: &firstMemoUID})
	require.NoError(t, err)
	require.NotNil(t, firstStoreMemo)

	var updatedExport *store.MemoExport
	for _, item := range exports {
		if item.MemoID == firstStoreMemo.ID {
			updatedExport = item
			break
		}
	}
	require.NotNil(t, updatedExport)
	require.Equal(t, updatedMemoTime.Unix(), updatedExport.UpdatedTs)
}

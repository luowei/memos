package v1

import (
	"context"

	"github.com/pkg/errors"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/usememos/memos/store"
)

type memoExportMetadataResponse struct {
	ExportTs *int64 `json:"exportTs,omitempty"`
}

func (s *APIV1Service) getMemoExportMetadata(ctx context.Context, memoName string) (*memoExportMetadataResponse, error) {
	memo, err := s.getAccessibleMemo(ctx, memoName)
	if err != nil {
		return nil, err
	}

	exports, err := s.Store.ListMemoExports(ctx, &store.FindMemoExport{MemoID: &memo.ID})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to list memo export metadata: %v", err)
	}

	response := &memoExportMetadataResponse{}
	if len(exports) > 0 {
		response.ExportTs = &exports[0].ExportTs
	}
	return response, nil
}

func (s *APIV1Service) getAccessibleMemo(ctx context.Context, memoName string) (*store.Memo, error) {
	memoUID, err := ExtractMemoUIDFromName(memoName)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid memo name: %v", err)
	}
	memo, err := s.Store.GetMemo(ctx, &store.FindMemo{UID: &memoUID})
	if err != nil {
		return nil, errors.Wrap(err, "failed to get memo")
	}
	if memo == nil {
		return nil, status.Errorf(codes.NotFound, "memo not found")
	}

	if memo.RowStatus == store.Archived {
		user, err := s.fetchCurrentUser(ctx)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to get user")
		}
		if user == nil || memo.CreatorID != user.ID {
			return nil, status.Errorf(codes.NotFound, "memo not found")
		}
	}

	if memo.Visibility != store.Public {
		user, err := s.fetchCurrentUser(ctx)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to get user")
		}
		if user == nil {
			return nil, status.Errorf(codes.Unauthenticated, "user not authenticated")
		}
		if memo.Visibility == store.Private && memo.CreatorID != user.ID {
			return nil, status.Errorf(codes.PermissionDenied, "permission denied")
		}
	}

	return memo, nil
}

package v1

import (
	"context"

	"github.com/pkg/errors"

	"github.com/usememos/memos/store"
)

func (s *APIV1Service) syncMemoExportUpdatedTs(ctx context.Context, memoID int32, updatedTs int64) error {
	exports, err := s.Store.ListMemoExports(ctx, &store.FindMemoExport{MemoID: &memoID})
	if err != nil {
		return errors.Wrap(err, "failed to list memo export metadata")
	}
	if len(exports) == 0 {
		return nil
	}

	if err := s.Store.UpdateMemoExport(ctx, &store.UpdateMemoExport{
		MemoID:    memoID,
		UpdatedTs: &updatedTs,
	}); err != nil {
		return errors.Wrap(err, "failed to update memo export metadata")
	}
	return nil
}

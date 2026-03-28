package store

import "context"

type MemoExport struct {
	MemoID    int32
	ExportTs  int64
	CreatedTs int64
	UpdatedTs int64
}

type FindMemoExport struct {
	MemoID     *int32
	MemoIDList []int32
}

type DeleteMemoExport struct {
	MemoID *int32
}

type UpdateMemoExport struct {
	MemoID    int32
	ExportTs  *int64
	UpdatedTs *int64
}

func (s *Store) UpsertMemoExport(ctx context.Context, memoExport *MemoExport) (*MemoExport, error) {
	return s.driver.UpsertMemoExport(ctx, memoExport)
}

func (s *Store) ListMemoExports(ctx context.Context, find *FindMemoExport) ([]*MemoExport, error) {
	return s.driver.ListMemoExports(ctx, find)
}

func (s *Store) UpdateMemoExport(ctx context.Context, update *UpdateMemoExport) error {
	return s.driver.UpdateMemoExport(ctx, update)
}

func (s *Store) DeleteMemoExport(ctx context.Context, delete *DeleteMemoExport) error {
	return s.driver.DeleteMemoExport(ctx, delete)
}

package mysql

import (
	"context"
	"strings"

	"github.com/usememos/memos/store"
)

func (d *DB) UpsertMemoExport(ctx context.Context, memoExport *store.MemoExport) (*store.MemoExport, error) {
	if _, err := d.db.ExecContext(ctx, `
		INSERT INTO memo_export (memo_id, export_ts)
		VALUES (?, ?)
		ON DUPLICATE KEY UPDATE
			export_ts = VALUES(export_ts),
			updated_ts = CURRENT_TIMESTAMP
	`, memoExport.MemoID, memoExport.ExportTs); err != nil {
		return nil, err
	}

	list, err := d.ListMemoExports(ctx, &store.FindMemoExport{MemoID: &memoExport.MemoID})
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, nil
	}
	return list[0], nil
}

func (d *DB) ListMemoExports(ctx context.Context, find *store.FindMemoExport) ([]*store.MemoExport, error) {
	where, args := []string{"1 = 1"}, []any{}
	if find.MemoID != nil {
		where, args = append(where, "memo_id = ?"), append(args, *find.MemoID)
	}
	if len(find.MemoIDList) > 0 {
		placeholders := make([]string, 0, len(find.MemoIDList))
		for _, memoID := range find.MemoIDList {
			placeholders = append(placeholders, "?")
			args = append(args, memoID)
		}
		where = append(where, "memo_id IN ("+strings.Join(placeholders, ", ")+")")
	}

	rows, err := d.db.QueryContext(ctx, `
		SELECT memo_id, export_ts, UNIX_TIMESTAMP(created_ts), UNIX_TIMESTAMP(updated_ts)
		FROM memo_export
		WHERE `+strings.Join(where, " AND ")+`
		ORDER BY memo_id ASC
	`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := []*store.MemoExport{}
	for rows.Next() {
		memoExport := &store.MemoExport{}
		if err := rows.Scan(&memoExport.MemoID, &memoExport.ExportTs, &memoExport.CreatedTs, &memoExport.UpdatedTs); err != nil {
			return nil, err
		}
		list = append(list, memoExport)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}
	return list, nil
}

func (d *DB) UpdateMemoExport(ctx context.Context, update *store.UpdateMemoExport) error {
	set, args := []string{}, []any{}
	if update.ExportTs != nil {
		set, args = append(set, "export_ts = ?"), append(args, *update.ExportTs)
	}
	if update.UpdatedTs != nil {
		set, args = append(set, "updated_ts = FROM_UNIXTIME(?)"), append(args, *update.UpdatedTs)
	}
	if len(set) == 0 {
		return nil
	}
	args = append(args, update.MemoID)
	_, err := d.db.ExecContext(ctx, "UPDATE memo_export SET "+strings.Join(set, ", ")+" WHERE memo_id = ?", args...)
	return err
}

func (d *DB) DeleteMemoExport(ctx context.Context, delete *store.DeleteMemoExport) error {
	where, args := []string{"1 = 1"}, []any{}
	if delete.MemoID != nil {
		where, args = append(where, "memo_id = ?"), append(args, *delete.MemoID)
	}
	_, err := d.db.ExecContext(ctx, "DELETE FROM memo_export WHERE "+strings.Join(where, " AND "), args...)
	return err
}

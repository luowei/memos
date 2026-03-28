package store

import (
	"context"

	"github.com/pkg/errors"
)

func (s *Store) UpsertCustomInstanceSetting(ctx context.Context, name, value, description string) error {
	_, err := s.driver.UpsertInstanceSetting(ctx, &InstanceSetting{
		Name:        name,
		Value:       value,
		Description: description,
	})
	if err != nil {
		return errors.Wrap(err, "failed to upsert custom instance setting")
	}
	return nil
}

func (s *Store) GetCustomInstanceSetting(ctx context.Context, name string) (*InstanceSetting, error) {
	list, err := s.driver.ListInstanceSettings(ctx, &FindInstanceSetting{Name: name})
	if err != nil {
		return nil, errors.Wrap(err, "failed to list custom instance settings")
	}
	if len(list) == 0 {
		return nil, nil
	}
	if len(list) > 1 {
		return nil, errors.Errorf("found multiple custom instance settings with name %s", name)
	}
	return list[0], nil
}

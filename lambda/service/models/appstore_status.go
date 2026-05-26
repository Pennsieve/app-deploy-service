package models

import (
	"encoding/json"
	"fmt"
)

// AppStoreStatus represents an appstore application lifecycle status.
type AppStoreStatus string

const (
	AppStoreStatusActive   AppStoreStatus = "active"
	AppStoreStatusArchived AppStoreStatus = "archived"
)

func (s *AppStoreStatus) UnmarshalJSON(data []byte) error {
	var v string
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	switch AppStoreStatus(v) {
	case AppStoreStatusActive, AppStoreStatusArchived:
		*s = AppStoreStatus(v)
		return nil
	default:
		return fmt.Errorf("invalid AppStoreStatus %q: must be %q or %q", v, AppStoreStatusActive, AppStoreStatusArchived)
	}
}

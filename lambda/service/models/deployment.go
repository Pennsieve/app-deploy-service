package models

import "time"

type Deployment struct {
	Id            string    `json:"id"`
	ApplicationId string    `json:"applicationId"`
	InitiatedAt   time.Time `json:"initiatedAt"`
}

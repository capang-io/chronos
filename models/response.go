package models

type ResponseStatus struct {
	PrimaryKey string `json:"primary-key"`
	RowKey     int    `json:"row-key"`
	Status     string `json:"status"`
	Response   any    `json:"response,omitempty"`
	Error      string `json:"error,omitempty"`
}

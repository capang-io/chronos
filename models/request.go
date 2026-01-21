package models

// Configuration struct updated to use the MetadataItem slice.
type Configuration struct {
	PrimaryKey string         `json:"primarykey"`
	Protocol   string         `json:"protocol"`
	Host       string         `json:"host"`
	Path       string         `json:"path"`
	Port       string         `json:"port"`
	Metadata   []MetadataItem `json:"metadata"`
}

// MetadataItem struct represents the key/value pair inside the metadata array.
type MetadataItem struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

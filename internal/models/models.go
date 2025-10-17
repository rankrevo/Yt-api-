package models

import "time"

type ConversionQuality string

const (
	Quality64  ConversionQuality = "64"
	Quality128 ConversionQuality = "128"
	Quality192 ConversionQuality = "192"
	Quality256 ConversionQuality = "256"
	Quality320 ConversionQuality = "320"
)

type ConversionState string

const (
	StatePreparing   ConversionState = "preparing"
	StateFetching    ConversionState = "fetching_metadata"
	StateCreated     ConversionState = "created"
	StateDownloading ConversionState = "downloading"
	StateDownloaded  ConversionState = "downloaded"
	StateQueued      ConversionState = "initializing"
	StateConverting  ConversionState = "converting"
	StateCompleted   ConversionState = "completed"
	StateFailed      ConversionState = "failed"
)

type MetaLite struct {
	Title     string `json:"title"`
	Duration  int    `json:"duration"`
	Thumbnail string `json:"thumbnail"`
}

type ConversionSession struct {
	ID                 string            `json:"conversion_id"`
	URL                string            `json:"url"`
	AssetHash          string            `json:"asset_hash"`
	VariantHash        string            `json:"variant_hash"`
	State              ConversionState   `json:"status"`
	CreatedAt          time.Time         `json:"created_at"`
	UpdatedAt          time.Time         `json:"updated_at"`
	SourcePath         string            `json:"source_path"`
	OutputPath         string            `json:"output_path"`
	Quality            ConversionQuality `json:"quality"`
	Error              string            `json:"error"`
	Meta               MetaLite          `json:"metadata"`
}

type PrepareRequest struct {
	URL string `json:"url"`
}

type PrepareResponse struct {
	ConversionID string   `json:"conversion_id"`
	Status       string   `json:"status"`
	Metadata     MetaLite `json:"metadata"`
	Message      string   `json:"message"`
}

type ConvertRequest struct {
	ConversionID string            `json:"conversion_id"`
	Quality      ConversionQuality `json:"quality"`
	StartTime    string            `json:"start_time"`
	EndTime      string            `json:"end_time"`
}

type ConvertResponse struct {
	ConversionID string `json:"conversion_id"`
	Status       string `json:"status"`
	Message      string `json:"message"`
}

// ConvertAcceptedResponse is returned when a convert request is accepted
// asynchronously. Clients can check queue position and poll status.
type ConvertAcceptedResponse struct {
	ConversionID  string `json:"conversion_id"`
	Status        string `json:"status"`
	QueuePosition int    `json:"queue_position"`
	Message       string `json:"message"`
}

type StatusResponse struct {
	ConversionID       string `json:"conversion_id"`
	Status             string `json:"status"`
	DownloadURL        string `json:"download_url"`
	QueuePosition      int    `json:"queue_position,omitempty"`
	Error              string `json:"error,omitempty"`
}

package shared

import "time"

// NewsArticle 解析後的標準新聞對象
type NewsArticle struct {
	ID          string    `json:"id"`
	Source      string    `json:"source"`
	URL         string    `json:"url"`
	Title       string    `json:"title"`
	Author      string    `json:"author"`
	Content     string    `json:"content"`
	Description string    `json:"description"`
	PublishDate time.Time `json:"publish_date"`
	TraceID     string    `json:"trace_id"`
	FetchedAt   time.Time `json:"fetched_at"`
}

// ArchiveRecord 存於 S3 的物理存檔封裝
type ArchiveRecord struct {
	Fingerprint string         `json:"fingerprint"`
	URL         string         `json:"url"`
	Payload     string         `json:"payload"` // Gzip + Base64
	TraceID     string         `json:"trace_id"`
	Timestamp   time.Time      `json:"timestamp"`
	Metadata    map[string]any `json:"metadata"`
}

// StorageTask 發往隊列的輕量化存證任務
type StorageTask struct {
	ArticleID string    `json:"article_id"`
	S3Key     string    `json:"s3_key"`
	TraceID   string    `json:"trace_id"`
	CreatedAt time.Time `json:"created_at"`
}

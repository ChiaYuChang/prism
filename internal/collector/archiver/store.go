package archiver

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/ChiaYuChang/prism/internal/collector"
	"github.com/ChiaYuChang/prism/internal/storage"
)

type archiveStore struct {
	store  storage.Store
	logger *slog.Logger
}

func newArchiveStore(store storage.Store, logger *slog.Logger) (*archiveStore, error) {
	if store == nil {
		return nil, fmt.Errorf("%w: storage", ErrParamMissing)
	}
	if logger == nil {
		return nil, fmt.Errorf("%w: logger", ErrParamMissing)
	}
	return &archiveStore{store: store, logger: logger}, nil
}

func (a *archiveStore) Save(ctx context.Context, record collector.Archive) error {
	dateStr := record.Timestamp.Format("2006/01/02")
	archiveID := record.ID
	if archiveID == "" {
		archiveID = record.TraceID
	}
	base := "archives/" + dateStr + "/" + archiveID
	dataKey := base + ".data"
	metaKey := base + ".meta.json"

	if err := a.store.Put(ctx, dataKey, strings.NewReader(record.Payload), storage.PutOptions{}); err != nil {
		return fmt.Errorf("write payload %s: %w", dataKey, err)
	}
	metaBytes, err := buildMetaJSON(record)
	if err != nil {
		return fmt.Errorf("marshal metadata for %s: %w", record.TraceID, err)
	}
	if err := a.store.Put(ctx, metaKey, strings.NewReader(string(metaBytes)), storage.PutOptions{ContentType: "application/json"}); err != nil {
		return fmt.Errorf("write metadata %s: %w", metaKey, err)
	}
	return nil
}

func (a *archiveStore) Load(ctx context.Context, traceID string) (string, error) {
	dataKey, err := a.findKey(ctx, traceID, ".data")
	if err != nil {
		return "", err
	}
	metaKey := strings.TrimSuffix(dataKey, ".data") + ".meta.json"
	m, metaErr := a.readMeta(ctx, metaKey)
	if metaErr == nil && m.DeletedAt != nil {
		return "", fmt.Errorf("%w: trace_id=%s (soft-deleted at %s)", ErrNotFound, traceID, m.DeletedAt.Format(time.RFC3339))
	}

	body, err := a.readObject(ctx, dataKey)
	if err != nil {
		return "", fmt.Errorf("read archive %s: %w", dataKey, err)
	}
	if metaErr == nil {
		if m.PayloadSHA256 == "" {
			a.logger.ErrorContext(ctx, "archive meta is missing payload_sha256", slog.String("trace_id", traceID), slog.String("meta_key", metaKey))
			return "", fmt.Errorf("%w: trace_id=%s (no payload_sha256 in meta)", ErrCorrupted, traceID)
		}
		if actual := sha256Hex(body); actual != m.PayloadSHA256 {
			a.logger.ErrorContext(ctx, "archive integrity check failed", slog.String("trace_id", traceID), slog.String("expected", m.PayloadSHA256), slog.String("actual", actual))
			return "", fmt.Errorf("%w: trace_id=%s", ErrCorrupted, traceID)
		}
	}
	return string(body), nil
}

func (a *archiveStore) Scan(ctx context.Context, opts ScanOptions) ([]Meta, error) {
	objects, err := a.store.List(ctx, "archives/")
	if err != nil {
		return nil, fmt.Errorf("list archives: %w", err)
	}
	results := make([]Meta, 0)
	for _, object := range objects {
		if !strings.HasSuffix(object.Key, ".meta.json") {
			continue
		}
		if opts.TraceID != "" && !strings.HasSuffix(object.Key, "/"+opts.TraceID+".meta.json") {
			continue
		}
		m, err := a.readMeta(ctx, object.Key)
		if err != nil {
			a.logger.WarnContext(ctx, "skipping unreadable meta", slog.String("key", object.Key), slog.Any("error", err))
			continue
		}
		if !matchesScanOpts(m, opts) {
			continue
		}
		results = append(results, m)
		if opts.Limit > 0 && len(results) >= opts.Limit {
			break
		}
	}
	sort.Slice(results, func(i, j int) bool { return results[i].CreatedAt.Before(results[j].CreatedAt) })
	return results, nil
}

func (a *archiveStore) Remove(ctx context.Context, traceID string) error {
	keys, err := a.findKeys(ctx, traceID, ".meta.json")
	if err != nil {
		return err
	}
	for _, metaKey := range keys {
		data, err := a.readObject(ctx, metaKey)
		if err != nil {
			return fmt.Errorf("read meta %s: %w", metaKey, err)
		}
		updated, alreadyDeleted, err := stampDeletedAtInJSON(data, time.Now().UTC())
		if err != nil {
			return fmt.Errorf("stamp deleted_at in %s: %w", metaKey, err)
		}
		if alreadyDeleted {
			continue
		}
		if err := a.store.Put(ctx, metaKey, strings.NewReader(string(updated)), storage.PutOptions{ContentType: "application/json"}); err != nil {
			return fmt.Errorf("write meta %s: %w", metaKey, err)
		}
	}
	return nil
}

func (a *archiveStore) Purge(ctx context.Context, traceID string) error {
	keys, err := a.findKeys(ctx, traceID, ".data")
	if err != nil {
		return err
	}
	for _, dataKey := range keys {
		metaKey := strings.TrimSuffix(dataKey, ".data") + ".meta.json"
		data, err := a.readObject(ctx, metaKey)
		if err != nil {
			return fmt.Errorf("read meta before purge %s: %w", metaKey, err)
		}
		m, err := parseMeta(data, parseDateFromKeyPath(metaKey))
		if err != nil {
			return fmt.Errorf("parse meta before purge %s: %w", metaKey, err)
		}
		if m.DeletedAt == nil {
			return fmt.Errorf("refusing to purge non-soft-deleted archive: trace_id=%s (call Remove first)", traceID)
		}
		if err := a.store.Delete(ctx, dataKey); err != nil {
			return fmt.Errorf("purge data %s: %w", dataKey, err)
		}
		if err := a.store.Delete(ctx, metaKey); err != nil {
			return fmt.Errorf("purge meta %s: %w", metaKey, err)
		}
	}
	return nil
}

func (a *archiveStore) PurgeAll(ctx context.Context) (int, error) {
	objects, err := a.store.List(ctx, "archives/")
	if err != nil {
		return 0, fmt.Errorf("list archives: %w", err)
	}
	purged := 0
	for _, object := range objects {
		if !strings.HasSuffix(object.Key, ".meta.json") {
			continue
		}
		data, err := a.readObject(ctx, object.Key)
		if err != nil {
			return purged, fmt.Errorf("read meta during purge %s: %w", object.Key, err)
		}
		m, err := parseMeta(data, parseDateFromKeyPath(object.Key))
		if err != nil {
			return purged, fmt.Errorf("parse meta during purge %s: %w", object.Key, err)
		}
		if m.DeletedAt == nil {
			continue
		}
		dataKey := strings.TrimSuffix(object.Key, ".meta.json") + ".data"
		if err := a.store.Delete(ctx, dataKey); err != nil {
			return purged, fmt.Errorf("purge data %s: %w", dataKey, err)
		}
		if err := a.store.Delete(ctx, object.Key); err != nil {
			return purged, fmt.Errorf("purge meta %s: %w", object.Key, err)
		}
		purged++
	}
	return purged, nil
}

func (a *archiveStore) findKey(ctx context.Context, traceID, suffix string) (string, error) {
	keys, err := a.findKeys(ctx, traceID, suffix)
	if err != nil {
		return "", err
	}
	if len(keys) > 1 {
		a.logger.WarnContext(ctx, "multiple archive files found for trace ID, using first", slog.String("trace_id", traceID), slog.Int("count", len(keys)), slog.String("using", keys[0]))
	}
	return keys[0], nil
}

func (a *archiveStore) findKeys(ctx context.Context, traceID, suffix string) ([]string, error) {
	objects, err := a.store.List(ctx, "archives/")
	if err != nil {
		return nil, fmt.Errorf("list archives: %w", err)
	}
	needle := "/" + traceID + suffix
	keys := make([]string, 0)
	for _, object := range objects {
		if strings.HasSuffix(object.Key, needle) {
			keys = append(keys, object.Key)
		}
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("%w: trace_id=%s", ErrNotFound, traceID)
	}
	sort.Strings(keys)
	return keys, nil
}

func (a *archiveStore) readMeta(ctx context.Context, key string) (Meta, error) {
	data, err := a.readObject(ctx, key)
	if err != nil {
		return Meta{}, err
	}
	m, err := parseMeta(data, parseDateFromKeyPath(key))
	if err != nil {
		return Meta{}, fmt.Errorf("unmarshal meta %s: %w", key, err)
	}
	return m, nil
}

func (a *archiveStore) readObject(ctx context.Context, key string) ([]byte, error) {
	body, err := a.store.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	defer func() { _ = body.Close() }()
	data, err := io.ReadAll(body)
	if err != nil {
		return nil, fmt.Errorf("read storage object %s: %w", key, err)
	}
	return data, nil
}

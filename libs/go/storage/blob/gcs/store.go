// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package gcs

import (
	"context"
	"errors"
	"io"
	"strings"
	"time"

	gcsapi "cloud.google.com/go/storage"
	"google.golang.org/api/iterator"

	"mindclade.internal/libs/go/faults"
	"mindclade.internal/libs/go/identifiers"
	"mindclade.internal/libs/go/storage/blob"
)

type Store struct {
	bucket             *gcsapi.BucketHandle
	bucketName         string
	prefix             string
	temporaryDirectory string
	maximumObjectBytes int64
	writerChunkSize    int
	chunkRetryDeadline time.Duration
}

var _ blob.Store = (*Store)(nil)

func New(client *gcsapi.Client, bucket string, options ...Option) (*Store, error) {
	if client == nil || strings.TrimSpace(bucket) == "" || strings.TrimSpace(bucket) != bucket {
		return nil, faults.New(faults.CodeInvalidArgument, "invalid GCS blob configuration", faults.WithReason("invalid_gcs_blob_config"), faults.WithOperation("storage.blob.gcs.New"), faults.WithRetryPolicy(faults.NoRetry()))
	}
	return NewBucket(client.Bucket(bucket), bucket, options...)
}

// NewBucket constructs a store from an existing bucket handle. The bucket name
// is supplied separately because BucketHandle intentionally exposes no generic
// name accessor.
func NewBucket(bucket *gcsapi.BucketHandle, bucketName string, options ...Option) (*Store, error) {
	if bucket == nil || strings.TrimSpace(bucketName) == "" || strings.TrimSpace(bucketName) != bucketName {
		return nil, faults.New(faults.CodeInvalidArgument, "invalid GCS blob configuration", faults.WithReason("invalid_gcs_blob_config"), faults.WithOperation("storage.blob.gcs.NewBucket"), faults.WithRetryPolicy(faults.NoRetry()))
	}
	store := &Store{
		bucket:             bucket,
		bucketName:         bucketName,
		maximumObjectBytes: DefaultMaximumObjectBytes,
		writerChunkSize:    DefaultWriterChunkSize,
	}
	for _, option := range options {
		if option != nil {
			if err := option(store); err != nil {
				return nil, faults.Wrap(err, faults.CodeInvalidArgument, "invalid GCS blob configuration", faults.WithReason("invalid_gcs_blob_option"), faults.WithOperation("storage.blob.gcs.NewBucket"), faults.WithRetryPolicy(faults.NoRetry()))
			}
		}
	}
	return store, nil
}

func (store *Store) Put(ctx context.Context, key blob.Key, reader io.Reader, options blob.PutOptions) (blob.Attributes, error) {
	const operation = "storage.blob.gcs.Put"
	if ctx == nil || store == nil || store.bucket == nil || reader == nil {
		return blob.Attributes{}, faults.New(faults.CodeInvalidArgument, "invalid blob put request", faults.WithReason("invalid_blob_put_request"), faults.WithOperation(operation), faults.WithRetryPolicy(faults.NoRetry()))
	}
	if err := key.Validate(); err != nil {
		return blob.Attributes{}, err
	}
	if err := options.Validate(); err != nil {
		return blob.Attributes{}, err
	}

	staged, err := createSpool(ctx, reader, store.temporaryDirectory, store.maximumObjectBytes)
	if err != nil {
		return blob.Attributes{}, err
	}
	defer staged.Close()
	if !options.Digest.IsZero() && !options.Digest.Equal(staged.digest) {
		return blob.Attributes{}, faults.Wrap(blob.ErrDigestMismatch, faults.CodeDataLoss, "blob digest does not match content", faults.WithReason("blob_digest_mismatch"), faults.WithOperation(operation), faults.WithField("blob_key", key.String()), faults.WithContextMetadata(ctx), faults.WithRetryPolicy(faults.NoRetry()))
	}

	handle := store.object(key)
	intent := intentGeneral
	if options.Preconditions.IfNotExists {
		handle = handle.If(gcsapi.Conditions{DoesNotExist: true})
		intent = intentCreateOnly
	} else if generation := options.Preconditions.IfGenerationMatch; generation != nil {
		handle = handle.If(gcsapi.Conditions{GenerationMatch: *generation})
		intent = intentGenerationMatch
	}

	uploadCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	writer := handle.NewWriter(uploadCtx)
	writer.ContentType = options.ContentType
	writer.Metadata = make(map[string]string, len(options.Metadata)+1)
	for metadataKey, value := range options.Metadata {
		if strings.EqualFold(metadataKey, DigestMetadataKey) {
			return blob.Attributes{}, faults.New(faults.CodeInvalidArgument, "blob metadata uses a reserved key", faults.WithReason("reserved_blob_metadata_key"), faults.WithOperation(operation), faults.WithField("metadata_key", metadataKey), faults.WithRetryPolicy(faults.NoRetry()))
		}
		writer.Metadata[metadataKey] = value
	}
	writer.Metadata[DigestMetadataKey] = staged.digest.String()
	writer.ChunkSize = store.writerChunkSize
	writer.ChunkRetryDeadline = store.chunkRetryDeadline

	if _, err := io.Copy(writer, staged.file); err != nil {
		cancel()
		_ = writer.Close()
		return blob.Attributes{}, qualify(ctx, err, operation, store.bucketName, key, intent)
	}
	if err := writer.Close(); err != nil {
		return blob.Attributes{}, qualify(ctx, err, operation, store.bucketName, key, intent)
	}
	attributes, err := store.convertAttributes(writer.Attrs())
	if err != nil {
		return blob.Attributes{}, err
	}
	if attributes.Size != staged.size || !attributes.Digest.Equal(staged.digest) {
		return blob.Attributes{}, faults.Wrap(blob.ErrDigestMismatch, faults.CodeDataLoss, "stored blob integrity metadata is inconsistent", faults.WithReason("gcs_blob_integrity_mismatch"), faults.WithOperation(operation), faults.WithField("blob_key", key.String()), faults.WithContextMetadata(ctx), faults.WithRetryPolicy(faults.NoRetry()))
	}
	return attributes, nil
}

func (store *Store) Open(ctx context.Context, key blob.Key, options blob.GetOptions) (blob.Object, error) {
	const operation = "storage.blob.gcs.Open"
	if ctx == nil || store == nil || store.bucket == nil {
		return blob.Object{}, faults.New(faults.CodeInvalidArgument, "invalid blob open request", faults.WithReason("invalid_blob_open_request"), faults.WithOperation(operation), faults.WithRetryPolicy(faults.NoRetry()))
	}
	if err := key.Validate(); err != nil {
		return blob.Object{}, err
	}
	if err := options.Validate(); err != nil {
		return blob.Object{}, err
	}

	handle := store.object(key)
	if options.Generation != nil {
		handle = handle.Generation(*options.Generation)
	}
	providerAttributes, err := handle.Attrs(ctx)
	if err != nil {
		return blob.Object{}, qualify(ctx, err, operation, store.bucketName, key, intentGeneral)
	}
	// Pin the generation between metadata and content reads.
	handle = store.object(key).Generation(providerAttributes.Generation)
	length := options.Length
	if length == 0 {
		length = -1
	}
	reader, err := handle.NewRangeReader(ctx, options.Offset, length)
	if err != nil {
		return blob.Object{}, qualify(ctx, err, operation, store.bucketName, key, intentGeneral)
	}
	attributes, err := store.convertAttributes(providerAttributes)
	if err != nil {
		_ = reader.Close()
		return blob.Object{}, err
	}
	return blob.Object{Attributes: attributes, Body: reader}, nil
}

func (store *Store) Stat(ctx context.Context, key blob.Key) (blob.Attributes, error) {
	const operation = "storage.blob.gcs.Stat"
	if ctx == nil || store == nil || store.bucket == nil {
		return blob.Attributes{}, faults.New(faults.CodeInvalidArgument, "invalid blob stat request", faults.WithReason("invalid_blob_stat_request"), faults.WithOperation(operation), faults.WithRetryPolicy(faults.NoRetry()))
	}
	if err := key.Validate(); err != nil {
		return blob.Attributes{}, err
	}
	providerAttributes, err := store.object(key).Attrs(ctx)
	if err != nil {
		return blob.Attributes{}, qualify(ctx, err, operation, store.bucketName, key, intentGeneral)
	}
	return store.convertAttributes(providerAttributes)
}

func (store *Store) Delete(ctx context.Context, key blob.Key, options blob.DeleteOptions) error {
	const operation = "storage.blob.gcs.Delete"
	if ctx == nil || store == nil || store.bucket == nil {
		return faults.New(faults.CodeInvalidArgument, "invalid blob delete request", faults.WithReason("invalid_blob_delete_request"), faults.WithOperation(operation), faults.WithRetryPolicy(faults.NoRetry()))
	}
	if err := key.Validate(); err != nil {
		return err
	}
	if err := options.Validate(); err != nil {
		return err
	}
	handle := store.object(key)
	intent := intentGeneral
	if generation := options.Preconditions.IfGenerationMatch; generation != nil {
		handle = handle.If(gcsapi.Conditions{GenerationMatch: *generation})
		intent = intentGenerationMatch
	}
	if err := handle.Delete(ctx); err != nil {
		return qualify(ctx, err, operation, store.bucketName, key, intent)
	}
	return nil
}

func (store *Store) List(ctx context.Context, options blob.ListOptions) (blob.Page, error) {
	const operation = "storage.blob.gcs.List"
	if ctx == nil || store == nil || store.bucket == nil {
		return blob.Page{}, faults.New(faults.CodeInvalidArgument, "invalid blob list request", faults.WithReason("invalid_blob_list_request"), faults.WithOperation(operation), faults.WithRetryPolicy(faults.NoRetry()))
	}
	normalized, err := options.Normalized()
	if err != nil {
		return blob.Page{}, err
	}
	providerPrefix := store.prefix + normalized.Prefix
	query := &gcsapi.Query{Prefix: providerPrefix}
	if normalized.Cursor != "" {
		query.StartOffset = store.prefix + normalized.Cursor
	}
	iteratorValue := store.bucket.Objects(ctx, query)
	page := blob.Page{Objects: make([]blob.Attributes, 0, normalized.Limit)}
	for {
		providerAttributes, nextErr := iteratorValue.Next()
		if errors.Is(nextErr, iterator.Done) {
			break
		}
		if nextErr != nil {
			return blob.Page{}, qualify(ctx, nextErr, operation, store.bucketName, "", intentGeneral)
		}
		name := strings.TrimPrefix(providerAttributes.Name, store.prefix)
		if normalized.Cursor != "" && name <= normalized.Cursor {
			continue
		}
		attributes, conversionErr := store.convertAttributes(providerAttributes)
		if conversionErr != nil {
			return blob.Page{}, conversionErr
		}
		if len(page.Objects) == normalized.Limit {
			page.NextCursor = page.Objects[len(page.Objects)-1].Key.String()
			break
		}
		page.Objects = append(page.Objects, attributes)
	}
	return page, nil
}

func (store *Store) object(key blob.Key) *gcsapi.ObjectHandle {
	return store.bucket.Object(store.prefix + key.String())
}

func (store *Store) convertAttributes(value *gcsapi.ObjectAttrs) (blob.Attributes, error) {
	if store == nil || value == nil || !strings.HasPrefix(value.Name, store.prefix) {
		return blob.Attributes{}, faults.New(faults.CodeDataLoss, "invalid GCS object metadata", faults.WithReason("invalid_gcs_object_metadata"), faults.WithOperation("storage.blob.gcs.convertAttributes"), faults.WithRetryPolicy(faults.NoRetry()))
	}
	key, err := blob.ParseKey(strings.TrimPrefix(value.Name, store.prefix))
	if err != nil {
		return blob.Attributes{}, faults.Wrap(err, faults.CodeDataLoss, "invalid GCS object metadata", faults.WithReason("invalid_gcs_object_key"), faults.WithOperation("storage.blob.gcs.convertAttributes"), faults.WithRetryPolicy(faults.NoRetry()))
	}
	metadata := make(blob.Metadata, len(value.Metadata))
	var digest identifiers.Digest
	for metadataKey, metadataValue := range value.Metadata {
		if strings.EqualFold(metadataKey, DigestMetadataKey) {
			parsed, parseErr := identifiers.ParseDigest(metadataValue)
			if parseErr != nil {
				return blob.Attributes{}, faults.Wrap(parseErr, faults.CodeDataLoss, "invalid GCS object digest metadata", faults.WithReason("invalid_gcs_blob_digest"), faults.WithOperation("storage.blob.gcs.convertAttributes"), faults.WithField("blob_key", key.String()), faults.WithRetryPolicy(faults.NoRetry()))
			}
			digest = parsed
			continue
		}
		metadata[metadataKey] = metadataValue
	}
	if err := metadata.Validate(); err != nil {
		return blob.Attributes{}, faults.Wrap(err, faults.CodeDataLoss, "invalid GCS object metadata", faults.WithReason("invalid_gcs_blob_metadata"), faults.WithOperation("storage.blob.gcs.convertAttributes"), faults.WithField("blob_key", key.String()), faults.WithRetryPolicy(faults.NoRetry()))
	}
	attributes := blob.Attributes{
		Key:         key,
		Size:        value.Size,
		Digest:      digest,
		ContentType: value.ContentType,
		ETag:        value.Etag,
		Generation:  value.Generation,
		CreatedAt:   value.Created.Round(0),
		UpdatedAt:   value.Updated.Round(0),
		Metadata:    metadata,
	}
	if err := attributes.Validate(); err != nil {
		return blob.Attributes{}, faults.Wrap(err, faults.CodeDataLoss, "invalid GCS object metadata", faults.WithReason("invalid_gcs_blob_attributes"), faults.WithOperation("storage.blob.gcs.convertAttributes"), faults.WithField("blob_key", key.String()), faults.WithRetryPolicy(faults.NoRetry()))
	}
	return attributes, nil
}

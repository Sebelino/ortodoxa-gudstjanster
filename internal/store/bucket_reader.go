package store

import (
	"context"
	"io"
	"time"

	"cloud.google.com/go/storage"
	"google.golang.org/api/iterator"
)

// BucketReader provides read-only access to objects in a GCS bucket.
type BucketReader struct {
	client *storage.Client
	bucket string
}

// NewBucketReader creates a new BucketReader for the specified bucket.
func NewBucketReader(ctx context.Context, bucket string) (*BucketReader, error) {
	client, err := storage.NewClient(ctx)
	if err != nil {
		return nil, err
	}
	return &BucketReader{
		client: client,
		bucket: bucket,
	}, nil
}

// ListObjects returns the names of all objects under the given prefix.
func (r *BucketReader) ListObjects(ctx context.Context, prefix string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	it := r.client.Bucket(r.bucket).Objects(ctx, &storage.Query{Prefix: prefix})
	var names []string
	for {
		attrs, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}
		names = append(names, attrs.Name)
	}
	return names, nil
}

// ReadObject reads the contents of the named object.
func (r *BucketReader) ReadObject(ctx context.Context, name string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	reader, err := r.client.Bucket(r.bucket).Object(name).NewReader(ctx)
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	return io.ReadAll(reader)
}

// Close closes the underlying GCS client.
func (r *BucketReader) Close() error {
	return r.client.Close()
}

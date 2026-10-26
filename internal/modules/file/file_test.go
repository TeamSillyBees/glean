package file

import (
	"bytes"
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/teamsillybees/glean/internal/platform/storage"
)

func TestUploadBodyLimitIncludesMultipartOverhead(t *testing.T) {
	require.Equal(t, int64(11*1024*1024), uploadBodyLimit(10*1024*1024))
	require.Equal(t, int64(math.MaxInt64), uploadBodyLimit(math.MaxInt64))
}

func TestServiceUploadLocal(t *testing.T) {
	store := &fakeStorage{}
	service := NewService(store)

	vo, err := service.UploadLocal(context.Background(), "demo.txt", "text/plain", int64(len("hello")), bytes.NewBufferString("hello"))

	require.NoError(t, err)
	require.Equal(t, "local/demo.txt", vo.Key)
	require.Equal(t, "demo.txt", vo.FileName)
	require.Equal(t, "text/plain", vo.ContentType)
	require.Equal(t, int64(5), vo.Size)
	require.Equal(t, "hello", store.uploaded)
}

func TestServiceDownloadLocal(t *testing.T) {
	store := &fakeStorage{
		object: &storage.Object{
			Key:          "local/demo.txt",
			FileName:     "demo.txt",
			Size:         5,
			ContentType:  "text/plain",
			LastModified: time.Now(),
		},
		body: []byte("hello"),
	}
	service := NewService(store)

	result, err := service.DownloadLocal(context.Background(), "local/demo.txt")

	require.NoError(t, err)
	require.Equal(t, "demo.txt", result.Info.FileName)
	require.Equal(t, []byte("hello"), result.Body)
}

func TestServiceStatLocalMapsNotFound(t *testing.T) {
	service := NewService(&fakeStorage{err: storage.ErrNotFound})

	_, err := service.StatLocal(context.Background(), "missing.txt")

	require.Error(t, err)
	require.True(t, errors.Is(err, storage.ErrNotFound))
}

type fakeStorage struct {
	object   *storage.Object
	body     []byte
	uploaded string
	err      error
}

func (f *fakeStorage) Upload(_ context.Context, input storage.UploadInput) (*storage.Object, error) {
	if f.err != nil {
		return nil, f.err
	}
	buf := new(bytes.Buffer)
	if input.Body != nil {
		_, _ = buf.ReadFrom(input.Body)
	}
	f.uploaded = buf.String()
	return &storage.Object{
		Key:         "local/" + input.FileName,
		FileName:    input.FileName,
		Size:        int64(buf.Len()),
		ContentType: input.ContentType,
	}, nil
}

func (f *fakeStorage) DownloadBytes(_ context.Context, _ storage.DownloadInput) ([]byte, error) {
	if f.err != nil {
		return nil, f.err
	}
	return append([]byte(nil), f.body...), nil
}

func (f *fakeStorage) Delete(_ context.Context, _ storage.DeleteInput) error {
	return f.err
}

func (f *fakeStorage) Stat(_ context.Context, _ storage.ObjectInput) (*storage.Object, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.object, nil
}

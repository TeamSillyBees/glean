package httpdemo

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/teamsillybees/glean/internal/platform/httpclient"
)

func TestServiceGetHTTPBingo(t *testing.T) {
	client := &fakeHTTPClient{}
	service := NewService(client)

	vo, err := service.GetHTTPBingo(context.Background(), "hello")

	require.NoError(t, err)
	require.Equal(t, "/get", client.path)
	require.Equal(t, "hello", client.options.QueryParams.Get("message"))
	require.Empty(t, client.options.Headers)
	require.Equal(t, "GET", vo.Method)
	require.Equal(t, []string{"hello"}, vo.Args["message"])
}

func TestServiceGetHTTPBingoUsesDefaultMessage(t *testing.T) {
	client := &fakeHTTPClient{}
	service := NewService(client)

	_, err := service.GetHTTPBingo(context.Background(), "")

	require.NoError(t, err)
	require.Equal(t, defaultMessage, client.options.QueryParams.Get("message"))
}

func TestServiceGetHTTPBingoFormPage(t *testing.T) {
	client := &fakeHTTPClient{}
	service := NewService(client)

	vo, err := service.GetHTTPBingoFormPage(context.Background())

	require.NoError(t, err)
	require.Equal(t, "/forms/post", client.path)
	require.Empty(t, client.options.Headers)
	require.Equal(t, "text/html; charset=utf-8", vo.ContentType)
	require.Equal(t, "<form>demo</form>", vo.Body)
	require.Equal(t, int32(len("<form>demo</form>")), vo.Size)
}

func TestServiceMapsHTTPClientError(t *testing.T) {
	service := NewService(&fakeHTTPClient{
		err: &httpclient.Error{
			Kind:       httpclient.ErrorKindResponse,
			Service:    "httpbingo",
			StatusCode: http.StatusBadGateway,
			Message:    "bad gateway",
		},
	})

	_, err := service.GetHTTPBingo(context.Background(), "")

	require.Error(t, err)
	require.True(t, errors.Is(err, service.client.(*fakeHTTPClient).err))
}

type fakeHTTPClient struct {
	method  string
	path    string
	options httpclient.RequestOptions
	err     error
}

func (f *fakeHTTPClient) Do(_ context.Context, method string, path string, opts ...httpclient.RequestOption) (*httpclient.Response, error) {
	f.method = method
	f.path = path
	options, optionsErr := httpclient.ApplyRequestOptions(opts...)
	if optionsErr != nil {
		return nil, optionsErr
	}
	f.options = options
	if f.err != nil {
		return nil, f.err
	}
	if path == "/get" {
		payload := f.options.Result.(*httpBingoGetPayload)
		*payload = httpBingoGetPayload{
			Args: map[string][]string{
				"message": {f.options.QueryParams.Get("message")},
			},
			Method: "GET",
			Origin: "127.0.0.1",
			URL:    "https://httpbingo.org/get",
		}
		return &httpclient.Response{StatusCode: http.StatusOK, Result: payload}, nil
	}
	body := []byte("<form>demo</form>")
	return &httpclient.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": {"text/html; charset=utf-8"},
		},
		Body: body,
	}, nil
}
